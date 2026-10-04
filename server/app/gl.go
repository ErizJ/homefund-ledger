package app

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

// 科目预置按《住宅专项维修资金会计核算办法》（财会〔2020〕7号）附录一科目表：
// 商品住宅/公有住房分账的二级科目与办法"按资金性质分账核算"要求一致。
var glSeed = []glSubjectSeed{
	{"1001", "银行存款", "asset", ""},
	{"100101", "商品住宅维修资金专户", "asset", "1001"},
	{"100102", "公有住房维修资金专户", "asset", "1001"},
	{"100103", "国债专户", "asset", "1001"},
	{"1101", "国债投资", "asset", ""},
	{"110101", "商品住宅国债投资", "asset", "1101"},
	{"110102", "公有住房国债投资", "asset", "1101"},
	{"1201", "备用金", "asset", ""},
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
	{"4201", "经营收入", "income", ""},
	{"420101", "商品住宅经营收入", "income", "4201"},
	{"420102", "公有住房经营收入", "income", "4201"},
	{"4301", "共用设施处置收入", "income", ""},
	{"430101", "商品住宅处置收入", "income", "4301"},
	{"430102", "公有住房处置收入", "income", "4301"},
	{"4901", "其他收入", "income", ""},
	{"490101", "商品住宅其他收入", "income", "4901"},
	{"490102", "公有住房其他收入", "income", "4901"},
	{"5001", "维修支出", "expense", ""},
	{"500101", "工程维修费", "expense", "5001"},
	{"500102", "监理费", "expense", "5001"},
	{"500103", "检测费、勘察设计费", "expense", "5001"},
	{"500104", "其他维修相关费用", "expense", "5001"},
	{"5101", "返还支出", "expense", ""},
	{"510101", "商品住宅返还支出", "expense", "5101"},
	{"510102", "公有住房返还支出", "expense", "5101"},
	{"5901", "其他支出", "expense", ""},
	{"590101", "商品住宅其他支出", "expense", "5901"},
	{"590102", "公有住房其他支出", "expense", "5901"},
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
		case "refund":
			return "510102"
		case "bond_inv":
			return "110102"
		case "bond_interest":
			return "410202"
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
	case "refund":
		return "510101"
	case "bond_inv":
		return "110101"
	case "bond_interest":
		return "410201"
	}
	switch slot {
	case "bond_bank":
		return "100103" // 国债专户（商品/公房共用）
	}
	return ""
}

// plNet 损益科目（4/5 类）在某一科目+项目下的借贷发生额净额汇总
type plNet struct {
	subject  string
	project  sql.NullInt64
	debit    int64
	credit   int64
	fundType string
}

// closingEntriesFromAggs 按科目+项目净额构造期末结转分录：
// 4 类收入净贷方 → 借收入/贷净资产（交存→维修资金）或待分配收益（利息处置等）；净借方（退返冲正）→ 反向。
// 5 类支出净借方 → 借净资产/贷支出；净贷方（冲正）→ 反向。
func closingEntriesFromAggs(aggs []plNet) []glEntry {
	entries := []glEntry{}
	for _, a := range aggs {
		pid := a.project
		fund := a.fundType
		if fund != "public" {
			fund = "commercial"
		}
		switch {
		case strings.HasPrefix(a.subject, "4"):
			slot := "pending"
			if strings.HasPrefix(a.subject, "4001") {
				slot = "netasset"
			}
			if nc := a.credit - a.debit; nc > 0 {
				entries = append(entries,
					glEntry{subject: a.subject, project: pid, dir: "debit", amount: nc},
					glEntry{subject: glSlot(fund, slot), project: pid, dir: "credit", amount: nc})
			} else if nc < 0 {
				entries = append(entries,
					glEntry{subject: glSlot(fund, slot), project: pid, dir: "debit", amount: -nc},
					glEntry{subject: a.subject, project: pid, dir: "credit", amount: -nc})
			}
		case strings.HasPrefix(a.subject, "5"):
			if nd := a.debit - a.credit; nd > 0 {
				entries = append(entries,
					glEntry{subject: glSlot(fund, "netasset"), project: pid, dir: "debit", amount: nd},
					glEntry{subject: a.subject, project: pid, dir: "credit", amount: nd})
			} else if nd < 0 {
				entries = append(entries,
					glEntry{subject: a.subject, project: pid, dir: "debit", amount: -nd},
					glEntry{subject: glSlot(fund, "netasset"), project: pid, dir: "credit", amount: -nd})
			}
		}
	}
	return entries
}

type glEntry struct {
	subject string
	project interface{} // 小区 id（辅助核算），nil 表示无
	dir     string      // debit | credit
	amount  int64       // 单位：分
	summary string      // 分录行摘要（表格式录入；自动凭证留空）
}

// glNextNoTx 财务凭证号 = 当月前缀 + 现有最大号 +1（MAX 口径，删除/作废造成断号时不会撞号）
func glNextNoTx(tx *sqlTx, month string, closing bool) (string, error) {
	prefix := "GL" + strings.ReplaceAll(month, "-", "")
	if closing {
		prefix = "JZ" + strings.ReplaceAll(month, "-", "")
	}
	var n int64
	if err := tx.QueryRow(`SELECT IFNULL(MAX(CAST(substr(no, ?, 99) AS INTEGER)), 0)
		FROM gl_vouchers WHERE no LIKE ?`, len(prefix)+2, prefix+"-%").Scan(&n); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%04d", prefix, n+1), nil
}

// 在业务事务内生成一张财务记账凭证（借贷必须平衡，金额为整数分），createdBy 记录制单人
func glInsertTx(tx *sqlTx, date, month, kind, sourceType string, sourceID int64, summary string, entries []glEntry, createdBy string) (int64, error) {
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
	res, err := tx.Exec(`INSERT INTO gl_vouchers(no,date,kind,source_type,source_id,summary,month,status,created_at,created_by)
		VALUES(?,?,?,?,?,?,?, 'normal', ?, ?)`,
		no, date, kind, sourceType, sourceID, summary, month, time.Now().Format(time.RFC3339), createdBy)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	for _, e := range entries {
		if _, err := tx.Exec(`INSERT INTO gl_entries(voucher_id,subject_code,project_id,direction,amount,summary)
			VALUES(?,?,?,?,?,?)`, id, e.subject, e.project, e.dir, e.amount, e.summary); err != nil {
			return 0, err
		}
	}
	return id, nil
}

// bizGLReq 业务单据 → 财务分录的生成参数
type bizGLReq struct {
	Type        string // income | interest | expense | refund | fund_income | cash | bond
	Date        string
	CommunityID int64
	Amount      int64  // 主金额（分）
	Extra       int64  // 附加金额（国债兑付的利息部分，分）
	Category    string // expense=费用类别 / refund=refund_kind / fund_income=income_kind / cash=cash_kind / bond=bond_kind
	PayMethod   string // expense 付款方式：""或 bank=银行；cash=备用金
	Summary     string
	BizID       int64
	CreatedBy   string
}

// 收入类别 → 科目基础编码（4201/4301/4901，按资金性质补 01/02 后缀）
var fundIncomeSubject = map[string]string{
	"business": "4201",
	"disposal": "4301",
	"other":    "4901",
}

var fundIncomeLabel = map[string]string{
	"business": "经营收入",
	"disposal": "共用设施处置收入",
	"other":    "其他收入",
}

// bizGLEntries 计算一张业务单据的财务分录（收付实现制）与摘要前缀，不落库
func bizGLEntries(r bizGLReq) ([]glEntry, string, error) {
	var fundType string
	if err := db.QueryRow(`SELECT fund_type FROM communities WHERE id=?`, r.CommunityID).Scan(&fundType); err != nil {
		return nil, "", err
	}
	switch r.Type {
	case "income": // 业主交存：借 银行存款专户 / 贷 交存收入
		return []glEntry{
			{subject: glSlot(fundType, "bank"), project: r.CommunityID, dir: "debit", amount: r.Amount},
			{subject: glSlot(fundType, "contrib"), project: r.CommunityID, dir: "credit", amount: r.Amount},
		}, "交存收入", nil
	case "interest": // 存款利息：借 银行存款专户 / 贷 存款利息收入
		return []glEntry{
			{subject: glSlot(fundType, "bank"), project: r.CommunityID, dir: "debit", amount: r.Amount},
			{subject: glSlot(fundType, "interest"), project: r.CommunityID, dir: "credit", amount: r.Amount},
		}, "存款利息", nil
	case "expense": // 维修支出：借 维修支出-明细 / 贷 银行存款专户（备用金支付时贷 1201）
		subj, ok := expenseCategorySubject[r.Category]
		if !ok {
			subj = "500104"
		}
		creditSubj := glSlot(fundType, "bank")
		payLabel := "银行"
		if r.PayMethod == "cash" {
			creditSubj = "1201"
			payLabel = "备用金"
		}
		return []glEntry{
			{subject: subj, project: r.CommunityID, dir: "debit", amount: r.Amount},
			{subject: creditSubj, project: r.CommunityID, dir: "credit", amount: r.Amount},
		}, "维修支出（" + payLabel + "支付）", nil
	case "refund": // r.Category 传 refund_kind：return=退返交存（冲减交存收入）；destroy=灭失返还（返还支出）
		if r.Category == "return" {
			return []glEntry{
				{subject: glSlot(fundType, "contrib"), project: r.CommunityID, dir: "debit", amount: r.Amount},
				{subject: glSlot(fundType, "bank"), project: r.CommunityID, dir: "credit", amount: r.Amount},
			}, "退返交存", nil
		}
		return []glEntry{
			{subject: glSlot(fundType, "refund"), project: r.CommunityID, dir: "debit", amount: r.Amount},
			{subject: glSlot(fundType, "bank"), project: r.CommunityID, dir: "credit", amount: r.Amount},
		}, "灭失返还", nil
	case "fund_income": // 经营/共用设施处置/其他收入：借 银行存款 / 贷 对应收入科目
		base, ok := fundIncomeSubject[r.Category]
		if !ok {
			base = "4201"
		}
		subj := base + "01"
		if fundType == "public" {
			subj = base + "02"
		}
		label := fundIncomeLabel[r.Category]
		if label == "" {
			label = "经营收入"
		}
		return []glEntry{
			{subject: glSlot(fundType, "bank"), project: r.CommunityID, dir: "debit", amount: r.Amount},
			{subject: subj, project: r.CommunityID, dir: "credit", amount: r.Amount},
		}, label, nil
	case "cash": // r.Category 传 cash_kind：withdraw=提取备用金；return=备用金退回银行
		if r.Category == "return" {
			return []glEntry{
				{subject: glSlot(fundType, "bank"), project: r.CommunityID, dir: "debit", amount: r.Amount},
				{subject: "1201", project: r.CommunityID, dir: "credit", amount: r.Amount},
			}, "备用金退回", nil
		}
		return []glEntry{
			{subject: "1201", project: r.CommunityID, dir: "debit", amount: r.Amount},
			{subject: glSlot(fundType, "bank"), project: r.CommunityID, dir: "credit", amount: r.Amount},
		}, "提取备用金", nil
	case "bond": // r.Category 传 bond_kind：buy=购买国债；redeem=到期兑付（本金 + 利息）
		if r.Category == "redeem" {
			total := r.Amount + r.Extra
			entries := []glEntry{
				{subject: glSlot(fundType, "bond_bank"), project: r.CommunityID, dir: "debit", amount: total},
				{subject: glSlot(fundType, "bond_inv"), project: r.CommunityID, dir: "credit", amount: r.Amount},
			}
			if r.Extra > 0 {
				entries = append(entries, glEntry{subject: glSlot(fundType, "bond_interest"), project: r.CommunityID, dir: "credit", amount: r.Extra})
			}
			return entries, "国债兑付（本金+利息）", nil
		}
		return []glEntry{
			{subject: glSlot(fundType, "bond_inv"), project: r.CommunityID, dir: "debit", amount: r.Amount},
			{subject: glSlot(fundType, "bond_bank"), project: r.CommunityID, dir: "credit", amount: r.Amount},
		}, "购买国债", nil
	}
	return nil, "", nil // allocate / interest_alloc_child（分摊到户）不产生财务分录
}

// rebuildCommunityDayGL 按"小区 × 日期"重建汇总记账凭证：
// 删除该小区当日的旧汇总凭证 → 汇总当日全部有效业务单据的借贷分录（同科目同方向合并）→ 生成一张记账凭证。
// 业务增删改后调用，幂等；source_type='agg'，source_id=小区 id。
func rebuildCommunityDayGL(tx *sqlTx, communityID int64, date string, createdBy string, force bool) error {
	if !force && !shouldAutoGL() {
		return nil // 已关闭自动生成记账凭证：日常业务只记业务台账
	}
	if _, err := tx.Exec(`DELETE FROM gl_entries WHERE voucher_id IN
		(SELECT id FROM gl_vouchers WHERE source_type='agg' AND source_id=? AND date=?)`, communityID, date); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM gl_vouchers WHERE source_type='agg' AND source_id=? AND date=?`,
		communityID, date); err != nil {
		return err
	}
	var cname string
	tx.QueryRow(`SELECT name FROM communities WHERE id=?`, communityID).Scan(&cname)

	rows, err := tx.Query(`SELECT id, type, amount, expense_category, refund_kind, biz_kind
		FROM vouchers WHERE community_id=? AND date=? AND status='normal' AND master_id IS NULL
		  AND type IN ('income','interest','expense','refund','fund_income','cash','bond','interest_alloc')
		ORDER BY id`, communityID, date)
	if err != nil {
		return err
	}
	type bizRow struct {
		id      int64
		vtype   string
		amount  int64
		cat     string
		refund  string
		bizKind string
	}
	items := []bizRow{}
	for rows.Next() {
		var b bizRow
		rows.Scan(&b.id, &b.vtype, &b.amount, &b.cat, &b.refund, &b.bizKind)
		items = append(items, b)
	}
	rows.Close()
	if len(items) == 0 {
		return nil
	}

	type aggKey struct {
		subject string
		project int64
		dir     string
	}
	agg := map[aggKey]int64{}
	order := []aggKey{}
	typeCount := map[string]int{}
	for _, it := range items {
		if it.vtype == "interest_alloc" {
			// 收益分配：借 待分配累计收益 / 贷 维修资金（净资产）
			var fundType string
			tx.QueryRow(`SELECT fund_type FROM communities WHERE id=?`, communityID).Scan(&fundType)
			entries := []glEntry{
				{subject: glSlot(fundType, "pending"), project: communityID, dir: "debit", amount: it.amount},
				{subject: glSlot(fundType, "netasset"), project: communityID, dir: "credit", amount: it.amount},
			}
			for _, e := range entries {
				p, _ := e.project.(int64)
				k := aggKey{e.subject, p, e.dir}
				if _, ok := agg[k]; !ok {
					order = append(order, k)
				}
				agg[k] += e.amount
			}
			typeCount["收益分配"]++
			continue
		}
		extra := int64(0)
		if it.vtype == "bond" && it.bizKind == "redeem" {
			// 兑付利息 = 该主凭证挂载的利息业务凭证金额
			tx.QueryRow(`SELECT IFNULL(SUM(amount),0) FROM vouchers WHERE master_id=? AND status='normal'`, it.id).Scan(&extra)
		}
		// 类别字段按凭证类型取对应列：refund→refund_kind；fund_income/cash/bond→biz_kind
		cat := it.cat
		switch it.vtype {
		case "refund":
			cat = it.refund
		case "fund_income", "cash", "bond":
			cat = it.bizKind
		}
		req := bizGLReq{Type: it.vtype, Date: date, CommunityID: communityID, Amount: it.amount,
			Extra: extra, Category: cat, PayMethod: "", Summary: ""}
		if it.vtype == "expense" && it.cat == "cash" {
			// 兼容历史：费用类别为 cash 的支出按备用金支付处理
			req.Category = "other"
			req.PayMethod = "cash"
		}
		entries, label, err := bizGLEntries(req)
		if err != nil {
			return err
		}
		for _, e := range entries {
			p, _ := e.project.(int64)
			k := aggKey{e.subject, p, e.dir}
			if _, ok := agg[k]; !ok {
				order = append(order, k)
			}
			agg[k] += e.amount
		}
		if label != "" {
			typeCount[label]++
		}
	}

	if len(agg) == 0 {
		return nil
	}
	entries := make([]glEntry, 0, len(agg))
	for _, k := range order {
		entries = append(entries, glEntry{subject: k.subject, project: k.project, dir: k.dir, amount: agg[k]})
	}
	// 摘要：各业务类型笔数
	parts := []string{}
	for _, t := range []string{"交存收入", "存款利息", "维修支出（银行支付）", "维修支出（备用金支付）", "退返交存", "灭失返还", "经营收入", "共用设施处置收入", "其他收入", "提取备用金", "备用金退回", "购买国债", "国债兑付（本金+利息）", "收益分配"} {
		if n := typeCount[t]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s%d笔", t, n))
		}
	}
	summary := "记账凭证汇总｜" + cname + "｜" + strings.Join(parts, "、")
	if _, err := glInsertTx(tx, date, date[:7], "business", "agg", communityID, summary, entries, createdBy); err != nil {
		return err
	}
	return nil
}

// generateBusinessGL 业务单据落账后按小区×日期重建汇总记账凭证（受 auto_gl 开关控制）
func generateBusinessGL(tx *sqlTx, r bizGLReq) error {
	return rebuildCommunityDayGL(tx, r.CommunityID, r.Date, r.CreatedBy, false)
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

// GET /api/gl/entries?subject=&projectId=&from=&to=&detail=1  明细账
// 默认按"科目+小区"汇总（借方/贷方发生合计、净发生额、笔数）；detail=1 时返回逐笔明细。
func glEntries(c *gin.Context) {
	subject := c.Query("subject")
	projectID := c.Query("projectId")
	from, to := c.Query("from"), c.Query("to")
	if c.Query("detail") == "1" {
		glEntriesDetail(c, subject, projectID, from, to)
		return
	}
	sqlStr := `SELECT e.subject_code, s.name, IFNULL(cm.name,'合计（未指定小区）'),
		IFNULL(SUM(CASE e.direction WHEN 'debit' THEN e.amount ELSE 0 END),0),
		IFNULL(SUM(CASE e.direction WHEN 'credit' THEN e.amount ELSE 0 END),0),
		COUNT(*)
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
	sqlStr += ` GROUP BY e.subject_code, e.project_id ORDER BY e.subject_code, cm.name`
	rows, err := db.Query(sqlStr, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var code, name, project string
		var debit, credit int64
		var cnt int
		rows.Scan(&code, &name, &project, &debit, &credit, &cnt)
		out = append(out, gin.H{
			"subject": code, "subjectName": name, "project": project,
			"debit": centsToYuan(debit), "credit": centsToYuan(credit),
			"net": centsToYuan(debit - credit), "count": cnt,
		})
	}
	c.JSON(http.StatusOK, out)
}

// glEntriesDetail 逐笔明细（原明细账视图）
func glEntriesDetail(c *gin.Context, subject, projectID, from, to string) {
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

		// 业务台账：户账合计 = 期初 + 交存 + 利息分配 - 分摊 - 返还；公共账 = 公共账期初 + 利息 - 已分配利息
		var bizHousehold, bizPublic, publicOpening int64
		db.QueryRow(`SELECT IFNULL(SUM(h.opening_balance),0)
			+ IFNULL((SELECT SUM(CASE v.type WHEN 'income' THEN v.amount WHEN 'interest_alloc_child' THEN v.amount WHEN 'allocate' THEN -v.amount WHEN 'refund' THEN -v.amount ELSE 0 END)
				FROM vouchers v WHERE v.status='normal' AND v.date<=? AND v.household_id IN
				(SELECT h2.id FROM households h2 JOIN buildings b2 ON h2.building_id=b2.id WHERE b2.community_id=?)),0)
			FROM households h JOIN buildings b ON h.building_id=b.id WHERE b.community_id=?`,
			end, cm.id, cm.id).Scan(&bizHousehold)
		db.QueryRow(`SELECT public_opening FROM communities WHERE id=?`, cm.id).Scan(&publicOpening)
		db.QueryRow(`SELECT IFNULL(SUM(CASE v.type WHEN 'interest' THEN v.amount WHEN 'fund_income' THEN v.amount WHEN 'interest_alloc' THEN -v.amount ELSE 0 END),0)
			FROM vouchers v WHERE v.status='normal' AND v.community_id=? AND v.date<=?`,
			cm.id, end).Scan(&bizPublic)
		bizPublic += publicOpening

		// 期初建账凭证是否已生成
		var openingCnt int
		db.QueryRow(`SELECT COUNT(*) FROM gl_vouchers WHERE source_type='opening' AND source_id=? AND status='normal'`, cm.id).Scan(&openingCnt)

		out = append(out, gin.H{
			"communityId": cm.id, "community": cm.name, "fundType": cm.fundType,
			"glNetAsset": centsToYuan(glNet), "bizHousehold": centsToYuan(bizHousehold),
			"netDiff":   centsToYuan(glNet - bizHousehold),
			"glPending": centsToYuan(glPending), "bizPublic": centsToYuan(bizPublic),
			"pendingDiff": centsToYuan(glPending - bizPublic),
			"glBank":      centsToYuan(glBank),
			"openingDone": openingCnt > 0,
		})
	}
	c.JSON(http.StatusOK, gin.H{"rows": out, "asOf": monthEnd(month)})
}

// POST /api/gl/opening-balance  {communityId, date?}  期初建账凭证：
// 借 银行存款（户账期初 + 公共账期初）/ 贷 净资产（户账期初）/ 贷 待分配收益（公共账期初）
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
	var publicOpening int64
	if err := db.QueryRow(`SELECT fund_type, name, public_opening FROM communities WHERE id=?`, req.CommunityID).Scan(&fundType, &name, &publicOpening); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "小区不存在"})
		return
	}
	var opening int64
	db.QueryRow(`SELECT IFNULL(SUM(h.opening_balance),0) FROM households h
		JOIN buildings b ON h.building_id=b.id WHERE b.community_id=?`, req.CommunityID).Scan(&opening)
	if opening+publicOpening <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该小区没有期初余额（户账期初 + 公共账期初均为 0），无需建账"})
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
	entries := []glEntry{
		{subject: glSlot(fundType, "bank"), project: req.CommunityID, dir: "debit", amount: opening + publicOpening},
	}
	if opening > 0 {
		entries = append(entries, glEntry{subject: glSlot(fundType, "netasset"), project: req.CommunityID, dir: "credit", amount: opening})
	}
	if publicOpening > 0 {
		entries = append(entries, glEntry{subject: glSlot(fundType, "pending"), project: req.CommunityID, dir: "credit", amount: publicOpening})
	}
	id, err := glInsertTx(tx, req.Date, req.Date[:7], "opening", "opening", req.CommunityID,
		"期初建账｜"+name, entries, c.GetString("authUser"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "glVoucherId": id, "amount": centsToYuan(opening + publicOpening)})
}

// GET /api/gl/vouchers?month=&from=&to=&status=  记账凭证列表（财务账）
// amount 为该凭证借方合计（贷方相同）；status 空表示全部
func glVoucherList(c *gin.Context) {
	month := c.Query("month")
	from, to := c.Query("from"), c.Query("to")
	status := c.Query("status")
	sqlStr := `SELECT v.id, v.no, v.date, v.kind, v.summary, v.month, v.status, v.source_type, v.source_id, v.appendix, v.created_by,
		(SELECT COUNT(*) FROM gl_entries e WHERE e.voucher_id=v.id) AS lines,
		(SELECT IFNULL(SUM(e.amount),0) FROM gl_entries e WHERE e.voucher_id=v.id AND e.direction='debit') AS amt
	FROM gl_vouchers v WHERE 1=1`
	args := []interface{}{}
	if month != "" {
		sqlStr += ` AND v.month = ?`
		args = append(args, month)
	}
	if from != "" {
		sqlStr += ` AND v.date >= ?`
		args = append(args, from)
	}
	if to != "" {
		sqlStr += ` AND v.date <= ?`
		args = append(args, to)
	}
	if status != "" {
		sqlStr += ` AND v.status = ?`
		args = append(args, status)
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
		var no, date, kind, summary, month2, status2, sourceType, createdBy string
		var lines, appendix int
		var sourceID, amount int64
		rows.Scan(&id, &no, &date, &kind, &summary, &month2, &status2, &sourceType, &sourceID, &appendix, &createdBy, &lines, &amount)
		out = append(out, gin.H{"id": id, "no": no, "date": date, "kind": kind,
			"summary": summary, "month": month2, "status": status2, "lines": lines,
			"sourceType": sourceType, "sourceId": sourceID, "appendix": appendix,
			"createdBy": createdBy, "amount": centsToYuan(amount)})
	}
	c.JSON(http.StatusOK, out)
}

// POST /api/gl/manual-voucher  手工填制记账凭证（借贷必须平衡）
// 表格式录入：每行 摘要/科目/借方金额/贷方金额 + 项目辅助核算；附单据数可选。
func glManualVoucher(c *gin.Context) {
	var req struct {
		Date     string `json:"date" binding:"required"`
		Summary  string `json:"summary"`
		Appendix int    `json:"appendix"`
		Entries  []struct {
			Summary     string  `json:"summary"`
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
	if msg := lockError(req.Date[:7]); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": msg})
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
		entries = append(entries, glEntry{subject: e.SubjectCode, project: project, dir: e.Direction, amount: cents, summary: strings.TrimSpace(e.Summary)})
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
	// 凭证头摘要：优先用户填写；否则取首行分录摘要
	header := strings.TrimSpace(req.Summary)
	if header == "" {
		for _, e := range entries {
			if e.summary != "" {
				header = e.summary
				break
			}
		}
	}
	if header == "" {
		header = "手工凭证"
	}
	tx, err := db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer tx.Rollback()
	id, err := glInsertTx(tx, req.Date, req.Date[:7], "business", "manual", 0, "手工凭证｜"+header, entries, c.GetString("authUser"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := invalidateClosingTx(tx, req.Date[:7]); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var no string
	db.QueryRow(`SELECT no FROM gl_vouchers WHERE id=?`, id).Scan(&no)
	if req.Appendix > 0 {
		db.Exec(`UPDATE gl_vouchers SET appendix=? WHERE id=?`, req.Appendix, id)
	}
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
	if msg := lockError(vdate[:7]); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": msg})
		return
	}
	tx, err := db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE gl_vouchers SET status='voided' WHERE id=?`, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := invalidateClosingTx(tx, vdate[:7]); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := tx.Commit(); err != nil {
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
	var appendix int
	err = db.QueryRow(`SELECT no, date, kind, source_type, source_id, summary, month, status, appendix
		FROM gl_vouchers WHERE id=?`, id).Scan(&no, &date, &kind, &sourceType, &sourceID, &summary, &month, &status, &appendix)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "凭证不存在"})
		return
	}
	rows, err2 := db.Query(`SELECT e.subject_code, s.name, IFNULL(cm.name,''), e.direction, e.amount, e.summary
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
		var code, name, project, dir, esummary string
		var amount int64
		rows.Scan(&code, &name, &project, &dir, &amount, &esummary)
		entries = append(entries, gin.H{
			"subject": code, "subjectName": name, "project": project,
			"direction": dir, "amount": centsToYuan(amount), "summary": esummary,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"id": id, "no": no, "date": date, "kind": kind,
		"sourceType": sourceType, "sourceId": sourceID,
		"summary": summary, "month": month, "status": status,
		"appendix": appendix, "entries": entries,
	})
}

// ==================== 历史补账 ====================

// glBackfill 历史补账：为尚无汇总记账凭证覆盖的（小区 × 日期）重建汇总记账凭证，
// 并使其所在月份的结转失效（补账后需重新结转）。
func glBackfill(c *gin.Context) {
	rows, err := db.Query(`
		SELECT DISTINCT v.community_id, v.date
		FROM vouchers v
		WHERE v.status='normal' AND v.master_id IS NULL
		  AND v.type IN ('income','interest','expense','refund','fund_income','cash','bond','interest_alloc')
		  AND NOT EXISTS (SELECT 1 FROM gl_vouchers gv
			WHERE gv.source_type='agg' AND gv.source_id=v.community_id AND gv.date=v.date AND gv.status='normal')
		ORDER BY v.date`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	type pair struct {
		cid  int64
		date string
	}
	pairs := []pair{}
	for rows.Next() {
		var p pair
		rows.Scan(&p.cid, &p.date)
		pairs = append(pairs, p)
	}
	rows.Close()

	tx, err := db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer tx.Rollback()
	user := c.GetString("authUser")
	backfilled := 0
	touchedMonths := map[string]bool{}
	for _, p := range pairs {
		if err := rebuildCommunityDayGL(tx, p.cid, p.date, user, true); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("补账 %s 失败: %v", p.date, err)})
			return
		}
		backfilled++
		touchedMonths[p.date[:7]] = true
	}
	for m := range touchedMonths {
		if err := invalidateClosingTx(tx, m); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "backfilled": backfilled, "skipped": []gin.H{}})
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
			"opening": centsToYuan(balFn(a.openD, a.openC)),
			"debit":   centsToYuan(a.cumD - a.openD),
			"credit":  centsToYuan(a.cumC - a.openC),
			"closing": centsToYuan(balFn(a.cumD, a.cumC)),
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
	if msg := lockError(req.Month); msg != "" {
		c.JSON(http.StatusConflict, gin.H{"error": msg})
		return
	}

	tx, err := db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer tx.Rollback()

	// 该月 4/5 类科目的借贷发生额（含此前手工结转产生的冲销分录，天然幂等），按净额结转
	nets := []plNet{}
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
		var n plNet
		rows.Scan(&n.subject, &n.project, &n.debit, &n.credit, &n.fundType)
		nets = append(nets, n)
	}
	rows.Close()

	entries := closingEntriesFromAggs(nets)
	if len(entries) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该月份没有需要结转的收入/支出（可能已全部结转）"})
		return
	}
	lastDate := req.Month + "-01"
	if t, e := time.Parse("2006-01", req.Month); e == nil {
		lastDate = t.AddDate(0, 1, -1).Format("2006-01-02")
	}
	no, err := glInsertTx(tx, lastDate, req.Month, "closing", "period", 0, "月末结转（手工）"+req.Month, entries, c.GetString("authUser"))
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

// ==================== 科目汇总表（记账凭证汇总） ====================

// GET /api/gl/voucher-summary?month=2026-09 或 ?year=2026
// 对齐原型「记账凭证-总」：按一级科目归集该期间全部 normal 记账凭证的借贷发生额与凭证张数。
// 月结/年结后在期末业务页展示、打印、导出。
func glVoucherSummary(c *gin.Context) {
	month, year := c.Query("month"), c.Query("year")
	var from, to, title string
	switch {
	case month != "":
		if len(month) != 7 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "月份格式应为 yyyy-MM"})
			return
		}
		from, to, title = month+"-01", monthEnd(month), month
	case year != "":
		if len(year) != 4 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "年份格式应为 yyyy"})
			return
		}
		from, to, title = year+"-01-01", year+"-12-31", year+" 年度"
	default:
		m := time.Now().Format("2006-01")
		from, to, title = m+"-01", monthEnd(m), m
	}

	rows, err := db.Query(`
		SELECT CASE WHEN s.parent != '' THEN s.parent ELSE s.code END AS acct,
			IFNULL(SUM(CASE e.direction WHEN 'debit'  THEN e.amount ELSE 0 END),0),
			IFNULL(SUM(CASE e.direction WHEN 'credit' THEN e.amount ELSE 0 END),0),
			COUNT(DISTINCT v.id)
		FROM gl_entries e
		JOIN gl_vouchers v ON e.voucher_id = v.id AND v.status='normal' AND v.date >= ? AND v.date <= ?
		JOIN gl_subjects s ON e.subject_code = s.code
		GROUP BY acct`, from, to)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	type agg struct {
		d, cr int64
		cnt   int
	}
	byCode := map[string]agg{}
	for rows.Next() {
		var code string
		var a agg
		rows.Scan(&code, &a.d, &a.cr, &a.cnt)
		byCode[code] = a
	}

	// 一级科目名称与类型
	type subj struct {
		code, name, typ string
	}
	srows, err := db.Query(`SELECT code, name, type FROM gl_subjects WHERE parent='' ORDER BY code`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	tops := []subj{}
	for srows.Next() {
		var s subj
		srows.Scan(&s.code, &s.name, &s.typ)
		tops = append(tops, s)
	}
	srows.Close()

	var totalD, totalC int64
	var totalCnt int
	out := []gin.H{}
	for _, s := range tops {
		a, ok := byCode[s.code]
		if !ok {
			continue
		}
		totalD += a.d
		totalC += a.cr
		totalCnt += a.cnt
		out = append(out, gin.H{
			"code": s.code, "name": s.name, "type": s.typ,
			"debit": centsToYuan(a.d), "credit": centsToYuan(a.cr), "count": a.cnt,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"title": title, "rows": out,
		"totalDebit": centsToYuan(totalD), "totalCredit": centsToYuan(totalC),
		"voucherCount": totalCnt, "balanced": totalD == totalC,
	})
}
