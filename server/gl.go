package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// sqlTx 是事务类型的短别名
type sqlTx = sql.Tx

// ==================== 科目预置（财会〔2020〕7号实务科目表） ====================

type glSubjectSeed struct {
	code   string
	name   string
	typ    string // asset | liability | net_asset | income | expense
	parent string
}

var glSeed = []glSubjectSeed{
	{"1001", "银行存款", "asset", ""},
	{"100101", "商品住宅维修资金专户", "asset", "1001"},
	{"100102", "公有住房维修资金专户", "asset", "1001"},
	{"100103", "国债专户", "asset", "1001"},
	{"1101", "国债投资", "asset", ""},
	{"110101", "商品住宅国债投资", "asset", "1101"},
	{"110102", "公有住房国债投资", "asset", "1101"},
	{"2001", "应付房屋灭失返还资金", "liability", ""},
	{"200101", "商品住宅应付返还资金", "liability", "2001"},
	{"200102", "公有住房应付返还资金", "liability", "2001"},
	{"3001", "商品住宅维修资金", "net_asset", ""},
	{"3002", "已售公有住房维修资金", "net_asset", ""},
	{"3101", "待分配累计收益", "net_asset", ""},
	{"310101", "商品住宅待分配收益", "net_asset", "3101"},
	{"310102", "公有住房待分配收益", "net_asset", "3101"},
	{"4001", "交存收入", "income", ""},
	{"400101", "商品住宅交存收入", "income", "4001"},
	{"400102", "公有住房交存收入", "income", "4001"},
	{"4101", "存款利息收入", "income", ""},
	{"410101", "商品住宅存款利息", "income", "4101"},
	{"410102", "公有住房存款利息", "income", "4101"},
	{"4102", "国债利息收入", "income", ""},
	{"410201", "商品住宅国债利息", "income", "4102"},
	{"410202", "公有住房国债利息", "income", "4102"},
	{"4103", "共用设施处置收入", "income", ""},
	{"410301", "商品住宅处置收入", "income", "4103"},
	{"410302", "公有住房处置收入", "income", "4103"},
	{"5001", "维修支出", "expense", ""},
	{"500101", "工程维修费", "expense", "5001"},
	{"500102", "监理费", "expense", "5001"},
	{"500103", "检测费、勘察设计费", "expense", "5001"},
	{"500104", "其他维修相关费用", "expense", "5001"},
	{"5101", "返还支出", "expense", ""},
	{"510101", "商品住宅返还支出", "expense", "5101"},
	{"510102", "公有住房返还支出", "expense", "5101"},
	{"5201", "其他支出", "expense", ""},
	{"520101", "商品住宅其他支出", "expense", "5201"},
	{"520102", "公有住房其他支出", "expense", "5201"},
}

func seedGLSubjects() {
	var cnt int
	db.QueryRow(`SELECT COUNT(*) FROM gl_subjects`).Scan(&cnt)
	if cnt >= len(glSeed) {
		return
	}
	for _, s := range glSeed {
		db.Exec(`INSERT OR IGNORE INTO gl_subjects(code,name,type,parent,enabled) VALUES(?,?,?,?,1)`,
			s.code, s.name, s.typ, s.parent)
	}
}

// 维修支出费用类别 → 科目
var expenseCategorySubject = map[string]string{
	"engineering": "500101",
	"supervision": "500102",
	"survey":      "500103",
	"other":       "500104",
}

// 按小区资金性质路由二级科目（商品住宅 / 公有住房）
func glSlot(fundType, slot string) string {
	if fundType == "public" {
		switch slot {
		case "bank":
			return "100102"
		case "contrib":
			return "400102"
		case "interest":
			return "410102"
		case "netasset":
			return "3002"
		case "pending":
			return "310102"
		}
	}
	switch slot {
	case "bank":
		return "100101"
	case "contrib":
		return "400101"
	case "interest":
		return "410101"
	case "netasset":
		return "3001"
	case "pending":
		return "310101"
	}
	return ""
}

type glEntry struct {
	subject string
	project interface{} // 小区 id（辅助核算），nil 表示无
	dir     string      // debit | credit
	amount  int64       // 单位：分
}

func glNextNoTx(tx *sqlTx, month string, closing bool) (string, error) {
	prefix := "GL" + strings.ReplaceAll(month, "-", "")
	if closing {
		prefix = "JZ" + strings.ReplaceAll(month, "-", "")
	}
	var cnt int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM gl_vouchers WHERE no LIKE ?`, prefix+"-%").Scan(&cnt); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%04d", prefix, cnt+1), nil
}

// 在业务事务内生成一张财务记账凭证（借贷必须平衡，金额为整数分）
func glInsertTx(tx *sqlTx, date, month, kind, sourceType string, sourceID int64, summary string, entries []glEntry) (int64, error) {
	var debit, credit int64
	for _, e := range entries {
		if e.dir == "debit" {
			debit += e.amount
		} else {
			credit += e.amount
		}
	}
	if debit != credit {
		return 0, fmt.Errorf("凭证借贷不平：借 %d 分 贷 %d 分", debit, credit)
	}
	no, err := glNextNoTx(tx, month, kind == "closing")
	if err != nil {
		return 0, err
	}
	res, err := tx.Exec(`INSERT INTO gl_vouchers(no,date,kind,source_type,source_id,summary,month,status,created_at)
		VALUES(?,?,?,?,?,?,?, 'normal', ?)`,
		no, date, kind, sourceType, sourceID, summary, month, time.Now().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	for _, e := range entries {
		if _, err := tx.Exec(`INSERT INTO gl_entries(voucher_id,subject_code,project_id,direction,amount)
			VALUES(?,?,?,?,?)`, id, e.subject, e.project, e.dir, e.amount); err != nil {
			return 0, err
		}
	}
	return id, nil
}

// 业务单据 → 记账凭证（收付实现制）
// vtype: income | interest | expense；category 仅 expense 用；amount 单位为分
func generateBusinessGL(tx *sqlTx, vtype, date string, communityID int64, amount int64, category, summary string, bizVoucherID int64) error {
	var fundType string
	if err := tx.QueryRow(`SELECT fund_type FROM communities WHERE id=?`, communityID).Scan(&fundType); err != nil {
		return err
	}
	month := date[:7]
	switch vtype {
	case "income": // 业主交存：借 银行存款专户 / 贷 交存收入
		_, err := glInsertTx(tx, date, month, "business", "voucher", bizVoucherID,
			"交存收入｜"+summary, []glEntry{
				{glSlot(fundType, "bank"), communityID, "debit", amount},
				{glSlot(fundType, "contrib"), communityID, "credit", amount},
			})
		return err
	case "interest": // 存款利息：借 银行存款专户 / 贷 存款利息收入
		_, err := glInsertTx(tx, date, month, "business", "voucher", bizVoucherID,
			"存款利息｜"+summary, []glEntry{
				{glSlot(fundType, "bank"), communityID, "debit", amount},
				{glSlot(fundType, "interest"), communityID, "credit", amount},
			})
		return err
	case "expense": // 维修支出：借 维修支出-明细 / 贷 银行存款专户
		subj, ok := expenseCategorySubject[category]
		if !ok {
			subj = "500104"
		}
		_, err := glInsertTx(tx, date, month, "business", "voucher", bizVoucherID,
			"维修支出｜"+summary, []glEntry{
				{subj, communityID, "debit", amount},
				{glSlot(fundType, "bank"), communityID, "credit", amount},
			})
		return err
	}
	return nil // allocate（分摊到户）不产生财务分录
}

// ==================== 财务报表与对账接口 ====================

func monthEnd(month string) string {
	if month == "" {
		return "9999-12-31"
	}
	t, err := time.Parse("2006-01", month)
	if err != nil {
		return "9999-12-31"
	}
	return t.AddDate(0, 1, -1).Format("2006-01-02")
}

// GET /api/gl/subjects
func glListSubjects(c *gin.Context) {
	rows, err := db.Query(`SELECT code, name, type, parent, enabled FROM gl_subjects ORDER BY code`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var code, name, typ, parent string
		var enabled int
		rows.Scan(&code, &name, &typ, &parent, &enabled)
		out = append(out, gin.H{"code": code, "name": name, "type": typ, "parent": parent, "enabled": enabled == 1})
	}
	c.JSON(http.StatusOK, out)
}

// GET /api/gl/balances?month=2026-10  截至月末的科目发生额与余额（二级科目按项目辅助核算展开）
func glBalances(c *gin.Context) {
	month := c.Query("month")
	end := monthEnd(month)
	rows, err := db.Query(`
		SELECT e.subject_code, s.name, s.type, s.parent, IFNULL(cm.name,'') AS project,
			IFNULL(SUM(CASE e.direction WHEN 'debit' THEN e.amount ELSE 0 END),0),
			IFNULL(SUM(CASE e.direction WHEN 'credit' THEN e.amount ELSE 0 END),0)
		FROM gl_entries e
		JOIN gl_vouchers v ON e.voucher_id = v.id AND v.status='normal' AND v.date <= ?
		JOIN gl_subjects s ON e.subject_code = s.code
		LEFT JOIN communities cm ON e.project_id = cm.id
		GROUP BY e.subject_code, e.project_id
		ORDER BY e.subject_code, project`, end)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var code, name, typ, parent, project string
		var d, cr int64
		rows.Scan(&code, &name, &typ, &parent, &project, &d, &cr)
		var balance int64
		dir := "debit"
		switch typ {
		case "asset", "expense":
			balance = d - cr
		default:
			balance = cr - d
			dir = "credit"
		}
		out = append(out, gin.H{
			"code": code, "name": name, "type": typ, "parent": parent,
			"project": project, "debit": centsToYuan(d), "credit": centsToYuan(cr),
			"balance": centsToYuan(balance), "balanceDir": dir,
		})
	}
	c.JSON(http.StatusOK, out)
}

// GET /api/gl/entries?subject=&projectId=&from=&to=  明细账
func glEntries(c *gin.Context) {
	subject := c.Query("subject")
	projectID := c.Query("projectId")
	from, to := c.Query("from"), c.Query("to")
	sqlStr := `SELECT e.id, v.no, v.date, v.kind, v.summary, e.subject_code, s.name,
		IFNULL(cm.name,''), e.direction, e.amount
	FROM gl_entries e
	JOIN gl_vouchers v ON e.voucher_id = v.id AND v.status='normal'
	JOIN gl_subjects s ON e.subject_code = s.code
	LEFT JOIN communities cm ON e.project_id = cm.id
	WHERE 1=1`
	args := []interface{}{}
	if subject != "" {
		sqlStr += ` AND e.subject_code LIKE ?`
		args = append(args, subject+"%")
	}
	if projectID != "" {
		sqlStr += ` AND e.project_id = ?`
		args = append(args, projectID)
	}
	if from != "" {
		sqlStr += ` AND v.date >= ?`
		args = append(args, from)
	}
	if to != "" {
		sqlStr += ` AND v.date <= ?`
		args = append(args, to)
	}
	sqlStr += ` ORDER BY v.date, v.id, e.id LIMIT 2000`
	rows, err := db.Query(sqlStr, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id int64
		var no, date, kind, summary, code, name, project, dir string
		var amount int64
		rows.Scan(&id, &no, &date, &kind, &summary, &code, &name, &project, &dir, &amount)
		out = append(out, gin.H{
			"id": id, "no": no, "date": date, "kind": kind,
			"summary": summary, "subject": code, "subjectName": name,
			"project": project, "direction": dir, "amount": centsToYuan(amount),
		})
	}
	c.JSON(http.StatusOK, out)
}

// GET /api/gl/trial-balance?month=  试算平衡
func glTrialBalance(c *gin.Context) {
	end := monthEnd(c.Query("month"))
	var d, cr int64
	err := db.QueryRow(`SELECT
		IFNULL(SUM(CASE e.direction WHEN 'debit' THEN e.amount ELSE 0 END),0),
		IFNULL(SUM(CASE e.direction WHEN 'credit' THEN e.amount ELSE 0 END),0)
	FROM gl_entries e JOIN gl_vouchers v ON e.voucher_id=v.id AND v.status='normal' AND v.date <= ?`, end).Scan(&d, &cr)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"debit": centsToYuan(d), "credit": centsToYuan(cr),
		"balanced": d == cr,
		"asOf":     end,
	})
}

// GET /api/gl/reconcile?month=  财务账 ↔ 业务台账对账
func glReconcile(c *gin.Context) {
	month := c.Query("month")
	end := monthEnd(month)
	communities := []struct {
		id       int64
		name     string
		fundType string
	}{}
	rows, err := db.Query(`SELECT id, name, fund_type FROM communities ORDER BY name`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	for rows.Next() {
		var cc struct {
			id       int64
			name     string
			fundType string
		}
		rows.Scan(&cc.id, &cc.name, &cc.fundType)
		communities = append(communities, cc)
	}
	rows.Close()

	out := []gin.H{}
	for _, cm := range communities {
		// 财务账：净资产（3001/3002 贷方余额）、待分配收益（3101）、银行存款（1001）
		var glNet, glPending, glBank int64
		db.QueryRow(`SELECT IFNULL(SUM(CASE e.direction WHEN 'credit' THEN e.amount ELSE -e.amount END),0)
			FROM gl_entries e JOIN gl_vouchers v ON e.voucher_id=v.id AND v.status='normal' AND v.date<=?
			WHERE e.project_id=? AND e.subject_code IN ('3001','3002')`, end, cm.id).Scan(&glNet)
		db.QueryRow(`SELECT IFNULL(SUM(CASE e.direction WHEN 'credit' THEN e.amount ELSE -e.amount END),0)
			FROM gl_entries e JOIN gl_vouchers v ON e.voucher_id=v.id AND v.status='normal' AND v.date<=?
			WHERE e.project_id=? AND e.subject_code IN ('310101','310102')`, end, cm.id).Scan(&glPending)
		db.QueryRow(`SELECT IFNULL(SUM(CASE e.direction WHEN 'debit' THEN e.amount ELSE -e.amount END),0)
			FROM gl_entries e JOIN gl_vouchers v ON e.voucher_id=v.id AND v.status='normal' AND v.date<=?
			WHERE e.project_id=? AND e.subject_code IN ('100101','100102')`, end, cm.id).Scan(&glBank)

		// 业务台账：户账合计 = 期初 + 交存 - 分摊；公共账 = 利息
		var bizHousehold, bizPublic int64
		db.QueryRow(`SELECT IFNULL(SUM(h.opening_balance),0)
			+ IFNULL((SELECT SUM(CASE v.type WHEN 'income' THEN v.amount WHEN 'allocate' THEN -v.amount ELSE 0 END)
				FROM vouchers v WHERE v.status='normal' AND v.date<=? AND v.household_id IN
				(SELECT h2.id FROM households h2 JOIN buildings b2 ON h2.building_id=b2.id WHERE b2.community_id=?)),0)
			FROM households h JOIN buildings b ON h.building_id=b.id WHERE b.community_id=?`,
			end, cm.id, cm.id).Scan(&bizHousehold)
		db.QueryRow(`SELECT IFNULL(SUM(v.amount),0) FROM vouchers v
			WHERE v.status='normal' AND v.type='interest' AND v.community_id=? AND v.date<=?`,
			cm.id, end).Scan(&bizPublic)

		// 期初建账凭证是否已生成
		var openingCnt int
		db.QueryRow(`SELECT COUNT(*) FROM gl_vouchers WHERE source_type='opening' AND source_id=? AND status='normal'`, cm.id).Scan(&openingCnt)

		out = append(out, gin.H{
			"communityId": cm.id, "community": cm.name, "fundType": cm.fundType,
			"glNetAsset": centsToYuan(glNet), "bizHousehold": centsToYuan(bizHousehold),
			"netDiff": centsToYuan(glNet - bizHousehold),
			"glPending": centsToYuan(glPending), "bizPublic": centsToYuan(bizPublic),
			"pendingDiff": centsToYuan(glPending - bizPublic),
			"glBank":      centsToYuan(glBank),
			"openingDone": openingCnt > 0,
		})
	}
	c.JSON(http.StatusOK, gin.H{"rows": out, "asOf": monthEnd(month)})
}

// POST /api/gl/opening-balance  {communityId, date?}  期初建账凭证：借 银行存款 / 贷 净资产
func glOpeningBalance(c *gin.Context) {
	var req struct {
		CommunityID int64  `json:"communityId" binding:"required"`
		Date        string `json:"date"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	if req.Date == "" {
		req.Date = time.Now().Format("2006-01-02")
	}
	if _, err := time.Parse("2006-01-02", req.Date); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "日期格式应为 yyyy-MM-dd"})
		return
	}
	var fundType string
	var name string
	if err := db.QueryRow(`SELECT fund_type, name FROM communities WHERE id=?`, req.CommunityID).Scan(&fundType, &name); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "小区不存在"})
		return
	}
	var opening int64
	db.QueryRow(`SELECT IFNULL(SUM(h.opening_balance),0) FROM households h
		JOIN buildings b ON h.building_id=b.id WHERE b.community_id=?`, req.CommunityID).Scan(&opening)
	if opening <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该小区没有期初余额，无需建账"})
		return
	}
	var cnt int
	db.QueryRow(`SELECT COUNT(*) FROM gl_vouchers WHERE source_type='opening' AND source_id=? AND status='normal'`, req.CommunityID).Scan(&cnt)
	if cnt > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "该小区期初建账凭证已存在"})
		return
	}
	tx, err := db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer tx.Rollback()
	id, err := glInsertTx(tx, req.Date, req.Date[:7], "opening", "opening", req.CommunityID,
		"期初建账｜"+name, []glEntry{
			{glSlot(fundType, "bank"), req.CommunityID, "debit", opening},
			{glSlot(fundType, "netasset"), req.CommunityID, "credit", opening},
		})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "glVoucherId": id, "amount": centsToYuan(opening)})
}

// GET /api/gl/vouchers?month=  记账凭证列表（财务账）
func glVoucherList(c *gin.Context) {
	month := c.Query("month")
	sqlStr := `SELECT v.id, v.no, v.date, v.kind, v.summary, v.month, v.status, v.source_type, v.source_id,
		(SELECT COUNT(*) FROM gl_entries e WHERE e.voucher_id=v.id) AS lines
	FROM gl_vouchers v WHERE 1=1`
	args := []interface{}{}
	if month != "" {
		sqlStr += ` AND v.month = ?`
		args = append(args, month)
	}
	sqlStr += ` ORDER BY v.date DESC, v.id DESC LIMIT 500`
	rows, err := db.Query(sqlStr, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id int64
		var no, date, kind, summary, month2, status, sourceType string
		var lines int
		var sourceID int64
		rows.Scan(&id, &no, &date, &kind, &summary, &month2, &status, &sourceType, &sourceID, &lines)
		out = append(out, gin.H{"id": id, "no": no, "date": date, "kind": kind,
			"summary": summary, "month": month2, "status": status, "lines": lines,
			"sourceType": sourceType, "sourceId": sourceID})
	}
	c.JSON(http.StatusOK, out)
}

// POST /api/gl/manual-voucher  手工填制记账凭证（借贷必须平衡）
func glManualVoucher(c *gin.Context) {
	var req struct {
		Date    string `json:"date" binding:"required"`
		Summary string `json:"summary"`
		Entries []struct {
			SubjectCode string  `json:"subjectCode" binding:"required"`
			ProjectID   int64   `json:"projectId"`
			Direction   string  `json:"direction" binding:"required"`
			Amount      float64 `json:"amount" binding:"required"`
		} `json:"entries" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误：" + err.Error()})
		return
	}
	if _, err := time.Parse("2006-01-02", req.Date); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "日期格式应为 yyyy-MM-dd"})
		return
	}
	if closed, err := isMonthClosed(req.Date[:7]); err == nil && closed {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该月份已月结锁账，请先反结转"})
		return
	}
	if len(req.Entries) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "至少需要两条分录（一借一贷）"})
		return
	}
	var debit, credit int64
	entries := make([]glEntry, 0, len(req.Entries))
	for _, e := range req.Entries {
		if e.Amount <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "分录金额必须大于 0"})
			return
		}
		if e.Direction != "debit" && e.Direction != "credit" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "分录方向只能是 debit 或 credit"})
			return
		}
		var cnt int
		if err := db.QueryRow(`SELECT COUNT(*) FROM gl_subjects WHERE code=? AND enabled=1`, e.SubjectCode).Scan(&cnt); err != nil || cnt == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "科目不存在或未启用：" + e.SubjectCode})
			return
		}
		var project interface{}
		if e.ProjectID > 0 {
			project = e.ProjectID
		}
		cents := yuanToCents(e.Amount)
		if cents <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "分录金额至少 0.01 元"})
			return
		}
		entries = append(entries, glEntry{e.SubjectCode, project, e.Direction, cents})
		if e.Direction == "debit" {
			debit += cents
		} else {
			credit += cents
		}
	}
	if debit != credit {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("借贷不平：借 %.2f ≠ 贷 %.2f", centsToYuan(debit), centsToYuan(credit))})
		return
	}
	tx, err := db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer tx.Rollback()
	id, err := glInsertTx(tx, req.Date, req.Date[:7], "business", "manual", 0, "手工凭证｜"+req.Summary, entries)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var no string
	db.QueryRow(`SELECT no FROM gl_vouchers WHERE id=?`, id).Scan(&no)
	c.JSON(http.StatusOK, gin.H{"ok": true, "id": id, "no": no})
}

// POST /api/gl/vouchers/:id/void  作废手工凭证（自动生成的凭证须通过业务凭证作废）
func glVoidManual(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效 id"})
		return
	}
	var sourceType, status string
	if err := db.QueryRow(`SELECT source_type, status FROM gl_vouchers WHERE id=?`, id).Scan(&sourceType, &status); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "凭证不存在"})
		return
	}
	if status != "normal" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该凭证已是作废状态"})
		return
	}
	if sourceType != "manual" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "业务/结转/期初凭证不能直接作废：业务凭证请在「凭证记账」里作废，结转凭证用反结转"})
		return
	}
	var vdate string
	db.QueryRow(`SELECT date FROM gl_vouchers WHERE id=?`, id).Scan(&vdate)
	if closed, err := isMonthClosed(vdate[:7]); err == nil && closed {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该月份已月结锁账，请先反结转"})
		return
	}
	if _, err := db.Exec(`UPDATE gl_vouchers SET status='voided' WHERE id=?`, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// GET /api/gl/vouchers/:id  记账凭证详情（含分录）
func glVoucherDetail(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效 id"})
		return
	}
	var no, date, kind, sourceType, summary, month, status string
	var sourceID int64
	err = db.QueryRow(`SELECT no, date, kind, source_type, source_id, summary, month, status
		FROM gl_vouchers WHERE id=?`, id).Scan(&no, &date, &kind, &sourceType, &sourceID, &summary, &month, &status)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "凭证不存在"})
		return
	}
	rows, err2 := db.Query(`SELECT e.subject_code, s.name, IFNULL(cm.name,''), e.direction, e.amount
		FROM gl_entries e JOIN gl_subjects s ON e.subject_code=s.code
		LEFT JOIN communities cm ON e.project_id=cm.id
		WHERE e.voucher_id=? ORDER BY e.id`, id)
	if err2 != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err2.Error()})
		return
	}
	defer rows.Close()
	entries := []gin.H{}
	for rows.Next() {
		var code, name, project, dir string
		var amount int64
		rows.Scan(&code, &name, &project, &dir, &amount)
		entries = append(entries, gin.H{
			"subject": code, "subjectName": name, "project": project,
			"direction": dir, "amount": centsToYuan(amount),
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"id": id, "no": no, "date": date, "kind": kind,
		"sourceType": sourceType, "sourceId": sourceID,
		"summary": summary, "month": month, "status": status, "entries": entries,
	})
}

// ==================== 历史补账 ====================

// glBackfill 为启用财务账套之前产生的、尚无对应财务凭证的 normal 业务凭证批量补账。
// 已月结锁账月份的凭证跳过（需先反结转再补），避免触碰已锁定的账期。
func glBackfill(c *gin.Context) {
	rows, err := db.Query(`
		SELECT v.id, v.date, v.type, v.community_id, v.amount, v.summary, v.expense_category
		FROM vouchers v
		WHERE v.status='normal' AND v.type IN ('income','interest','expense')
		  AND NOT EXISTS (SELECT 1 FROM gl_vouchers gv WHERE gv.source_type='voucher' AND gv.source_id=v.id)
		ORDER BY v.date, v.id`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	type item struct {
		id            int64
		date, vtype   string
		communityID   int64
		amount        int64
		summary, cat  string
	}
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.date, &it.vtype, &it.communityID, &it.amount, &it.summary, &it.cat); err != nil {
			rows.Close()
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		items = append(items, it)
	}
	rows.Close()

	tx, err := db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer tx.Rollback()
	backfilled := 0
	var skipped []gin.H
	for _, it := range items {
		month := it.date[:7]
		if closed, err := isMonthClosed(month); err == nil && closed {
			skipped = append(skipped, gin.H{"id": it.id, "date": it.date, "reason": "该月份已月结锁账，请先反结转"})
			continue
		}
		if err := generateBusinessGL(tx, it.vtype, it.date, it.communityID, it.amount, it.cat, it.summary, it.id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("补账凭证 #%d 失败: %v", it.id, err)})
			return
		}
		backfilled++
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "backfilled": backfilled, "skipped": skipped})
}

// ==================== 总分类账 ====================

// GET /api/gl/general-ledger?month=2026-10
// 总分类账：按一级科目汇总，期初余额（上月末累计）→ 本月借/贷发生 → 期末余额。
// 记账凭证（业务自动 + 手工）过账后即计入总账，收入/支出类科目在月末结转时并入净资产。
func glGeneralLedger(c *gin.Context) {
	month := c.Query("month")
	if len(month) < 7 {
		month = time.Now().Format("2006-01")
	}
	start := month[:7] + "-01"
	end := monthEnd(month)

	// 一级科目清单（有序）
	type subj struct {
		code, name, typ string
	}
	var tops []subj
	srows, err := db.Query(`SELECT code, name, type FROM gl_subjects WHERE parent='' ORDER BY code`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	for srows.Next() {
		var s subj
		srows.Scan(&s.code, &s.name, &s.typ)
		tops = append(tops, s)
	}
	srows.Close()

	// 按一级科目归集发生额：子科目并入父科目，无父科目的（3001/3002）直接归自己
	rows, err := db.Query(`
		SELECT CASE WHEN s.parent != '' THEN s.parent ELSE s.code END AS acct,
			IFNULL(SUM(CASE e.direction WHEN 'debit'  THEN e.amount ELSE 0 END),0),
			IFNULL(SUM(CASE e.direction WHEN 'credit' THEN e.amount ELSE 0 END),0),
			IFNULL(SUM(CASE WHEN v.date < ? AND e.direction='debit'  THEN e.amount ELSE 0 END),0),
			IFNULL(SUM(CASE WHEN v.date < ? AND e.direction='credit' THEN e.amount ELSE 0 END),0)
		FROM gl_entries e
		JOIN gl_vouchers v ON e.voucher_id = v.id AND v.status='normal' AND v.date <= ?
		JOIN gl_subjects s ON e.subject_code = s.code
		GROUP BY acct`, start, start, end)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	type agg4 struct{ cumD, cumC, openD, openC int64 }
	byCode := map[string]agg4{}
	for rows.Next() {
		var acct string
		var a agg4
		rows.Scan(&acct, &a.cumD, &a.cumC, &a.openD, &a.openC)
		byCode[acct] = a
	}

	out := []gin.H{}
	for _, s := range tops {
		a, ok := byCode[s.code]
		if !ok {
			a = agg4{}
		}
		isDebit := s.typ == "asset" || s.typ == "expense"
		dir, balFn := "debit", func(d, cc int64) int64 { return d - cc }
		if !isDebit {
			dir, balFn = "credit", func(d, cc int64) int64 { return cc - d }
		}
		out = append(out, gin.H{
			"code": s.code, "name": s.name, "type": s.typ, "dir": dir,
			"opening":  centsToYuan(balFn(a.openD, a.openC)),
			"debit":    centsToYuan(a.cumD - a.openD),
			"credit":   centsToYuan(a.cumC - a.openC),
			"closing":  centsToYuan(balFn(a.cumD, a.cumC)),
		})
	}
	c.JSON(http.StatusOK, gin.H{"month": month, "rows": out})
}

// ==================== 期末转账（手工结转） ====================

// POST /api/gl/transfer  {month: "2026-10"}
// 手工结转：不经过月结锁账，直接把该月收入/支出类科目余额结转入净资产。
// 采用净额结转（扣除已结转部分），可重复执行——每次只结转剩余损益；不写 periods 表、不锁月份。
func glTransferMonth(c *gin.Context) {
	var req struct {
		Month string `json:"month" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Month) < 7 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请提供月份，如 2026-10"})
		return
	}
	var closedCnt int
	db.QueryRow(`SELECT COUNT(*) FROM periods WHERE month=?`, req.Month).Scan(&closedCnt)
	if closedCnt > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "该月份已随月结锁账自动结转，无需手工结转"})
		return
	}

	tx, err := db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer tx.Rollback()

	// 该月 4/5 类科目的净余额（含此前手工结转产生的冲销分录，天然幂等）
	type net struct {
		subject  string
		project  sql.NullInt64
		debit    int64
		credit   int64
		fundType string
	}
	nets := []net{}
	rows, err := tx.Query(`
		SELECT e.subject_code, e.project_id,
			IFNULL(SUM(CASE WHEN e.direction='debit'  THEN e.amount ELSE 0 END),0),
			IFNULL(SUM(CASE WHEN e.direction='credit' THEN e.amount ELSE 0 END),0),
			IFNULL((SELECT fund_type FROM communities cm WHERE cm.id=e.project_id),'commercial')
		FROM gl_entries e
		JOIN gl_vouchers v ON e.voucher_id=v.id AND v.status='normal' AND substr(v.date,1,7)=?
		JOIN gl_subjects s ON e.subject_code=s.code
		WHERE substr(e.subject_code,1,1) IN ('4','5')
		GROUP BY e.subject_code, e.project_id`, req.Month)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	for rows.Next() {
		var n net
		rows.Scan(&n.subject, &n.project, &n.debit, &n.credit, &n.fundType)
		nets = append(nets, n)
	}
	rows.Close()

	lastDate := req.Month + "-01"
	if t, e := time.Parse("2006-01", req.Month); e == nil {
		lastDate = t.AddDate(0, 1, -1).Format("2006-01-02")
	}
	fund := func(n net) string {
		if n.fundType == "public" {
			return "public"
		}
		return "commercial"
	}
	entries := []glEntry{}
	for _, n := range nets {
		if strings.HasPrefix(n.subject, "4") {
			if nc := n.credit - n.debit; nc > 0 {
				slot := "pending"
				if strings.HasPrefix(n.subject, "4001") {
					slot = "netasset"
				}
				entries = append(entries, glEntry{n.subject, n.project, "debit", nc},
					glEntry{glSlot(fund(n), slot), n.project, "credit", nc})
			}
		} else if strings.HasPrefix(n.subject, "5") {
			if nd := n.debit - n.credit; nd > 0 {
				entries = append(entries, glEntry{glSlot(fund(n), "netasset"), n.project, "debit", nd},
					glEntry{n.subject, n.project, "credit", nd})
			}
		}
	}
	if len(entries) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该月份没有需要结转的收入/支出（可能已全部结转）"})
		return
	}
	no, err := glInsertTx(tx, lastDate, req.Month, "closing", "period", 0, "月末结转（手工）"+req.Month, entries)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成结转凭证失败：" + err.Error()})
		return
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "month": req.Month, "no": no})
}
