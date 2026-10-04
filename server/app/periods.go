package app

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// errNothingToClose 该月没有需要结转的收入/支出发生额
var errNothingToClose = errors.New("该月份没有收入/支出类财务发生额，无需月结")

// closeMonthTx 在事务内执行月结核心逻辑：
// 删除该月旧结转凭证（含手工期末转账，避免双重结转）→ 按科目+项目汇总当月损益净额 →
// 生成月末结转凭证（收入→净资产/待分配收益，支出→净资产，支持冲正反方向）→ 写入 periods 锁账。
func closeMonthTx(tx *sql.Tx, month, createdBy string) error {
	// 若该月已做过手工期末转账（gl/transfer），先删除其结转凭证，月结时统一重新生成，避免双重结转
	if _, err := tx.Exec(`DELETE FROM gl_entries WHERE voucher_id IN
		(SELECT id FROM gl_vouchers WHERE kind='closing' AND month=?)`, month); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM gl_vouchers WHERE kind='closing' AND month=?`, month); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM periods WHERE month=?`, month); err != nil {
		return err
	}

	// 汇总本月收入/支出类科目的借贷发生额（按科目+项目分组）
	aggs := []plNet{}
	rows, err := tx.Query(`
		SELECT e.subject_code, e.project_id,
			IFNULL(SUM(CASE WHEN e.direction='debit'  THEN e.amount ELSE 0 END),0),
			IFNULL(SUM(CASE WHEN e.direction='credit' THEN e.amount ELSE 0 END),0),
			IFNULL((SELECT fund_type FROM communities cm WHERE cm.id=e.project_id),'commercial')
		FROM gl_entries e
		JOIN gl_vouchers v ON e.voucher_id=v.id AND v.status='normal' AND substr(v.date,1,7)=?
		JOIN gl_subjects s ON e.subject_code=s.code
		WHERE substr(e.subject_code,1,1) IN ('4','5')
		GROUP BY e.subject_code, e.project_id`, month)
	if err != nil {
		return err
	}
	for rows.Next() {
		var a plNet
		rows.Scan(&a.subject, &a.project, &a.debit, &a.credit, &a.fundType)
		aggs = append(aggs, a)
	}
	rows.Close()
	if len(aggs) == 0 {
		return errNothingToClose
	}

	entries := closingEntriesFromAggs(aggs)
	lastDate := month + "-01"
	if t, e := time.Parse("2006-01", month); e == nil {
		lastDate = t.AddDate(0, 1, -1).Format("2006-01-02")
	}
	if _, err := glInsertTx(tx, lastDate, month, "closing", "period", 0, "月末结转 "+month, entries, createdBy); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO periods(month, closed_at) VALUES(?,?)`,
		month, time.Now().Format(time.RFC3339)); err != nil {
		return err
	}
	return nil
}

// reopenMonthTx 在事务内执行反结转：删除该月结转凭证并解除锁账
func reopenMonthTx(tx *sql.Tx, month string) error {
	if _, err := tx.Exec(`DELETE FROM gl_entries WHERE voucher_id IN
		(SELECT id FROM gl_vouchers WHERE kind='closing' AND month=?)`, month); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM gl_vouchers WHERE kind='closing' AND month=?`, month); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM periods WHERE month=?`, month); err != nil {
		return err
	}
	return nil
}

func isMonthClosed(month string) (bool, error) {
	var cnt int
	err := db.QueryRow(`SELECT COUNT(*) FROM periods WHERE month=?`, month).Scan(&cnt)
	return cnt > 0, err
}

func isYearClosed(year string) (bool, error) {
	var cnt int
	err := db.QueryRow(`SELECT COUNT(*) FROM fiscal_years WHERE year=?`, year).Scan(&cnt)
	return cnt > 0, err
}

// lockError 恒返回空串：按使用要求取消锁账，任何时候均可记账/作废；
// 改账后对应月份结转自动失效（invalidateClosingTx），需重新执行结转。
// 保留该函数以便日后如需恢复锁账时在各校验点一键启用。
func lockError(month string) string {
	return ""
}

// invalidateClosingTx 在改账事务内调用：清除某月的结转凭证与结转记录，
// 并撤销其所在年度的年结记录（改账后需要重新结转/年结）。
func invalidateClosingTx(tx *sql.Tx, month string) error {
	if _, err := tx.Exec(`DELETE FROM gl_entries WHERE voucher_id IN
		(SELECT id FROM gl_vouchers WHERE kind='closing' AND month=?)`, month); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM gl_vouchers WHERE kind='closing' AND month=?`, month); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM periods WHERE month=?`, month); err != nil {
		return err
	}
	if len(month) >= 4 {
		if _, err := tx.Exec(`DELETE FROM fiscal_years WHERE year=?`, month[:4]); err != nil {
			return err
		}
	}
	return nil
}

// GET /api/periods
func listPeriods(c *gin.Context) {
	rows, err := db.Query(`SELECT month, closed_at FROM periods ORDER BY month DESC`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var month, closedAt string
		rows.Scan(&month, &closedAt)
		out = append(out, gin.H{"month": month, "closedAt": closedAt})
	}
	c.JSON(http.StatusOK, out)
}

// POST /api/periods/close  {month: "2026-09"}
// 月末结转：生成/重生成结转凭证（收入→净资产；支出→净资产），不锁账，可重复执行（幂等）
func closePeriod(c *gin.Context) {
	var req struct {
		Month string `json:"month" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Month) != 7 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请提供月份，格式 yyyy-MM"})
		return
	}
	if _, err := time.Parse("2006-01", req.Month); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "月份格式应为 yyyy-MM"})
		return
	}

	tx, err := db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer tx.Rollback()
	if err := closeMonthTx(tx, req.Month, c.GetString("authUser")); err != nil {
		if errors.Is(err, errNothingToClose) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "该月份没有需要结转的收入/支出（如只有收益分配等内部结转，无需结转）"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// 月结成功后自动生成当月月报表与财务报表快照（会住维01/02/03表 Excel）；失败不影响月结结果
	resp := gin.H{"ok": true, "month": req.Month}
	if name, err := generateMonthlyReport(req.Month); err != nil {
		resp["reportError"] = "月报表生成失败：" + err.Error()
	} else {
		resp["report"] = name
	}
	if name, err := generateStatementsSnapshot(req.Month); err != nil {
		resp["statementReportError"] = "财务报表快照生成失败：" + err.Error()
	} else {
		resp["statementReport"] = name
	}
	c.JSON(http.StatusOK, resp)
}

// POST /api/periods/reopen  {month: "2026-09"}
// 反结转：解锁 + 删除该月结转凭证（下次月结重新生成）
func reopenPeriod(c *gin.Context) {
	var req struct {
		Month string `json:"month" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请提供月份"})
		return
	}
	var closedCnt int
	db.QueryRow(`SELECT COUNT(*) FROM periods WHERE month=?`, req.Month).Scan(&closedCnt)
	if closedCnt == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "该月份未月结"})
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
	if err := reopenMonthTx(tx, req.Month); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "month": req.Month})
}

// GET /api/vouchers/:id  凭证详情（含分摊子凭证与附件）
func voucherDetail(c *gin.Context) {
	id := c.Param("id")
	var v struct {
		id, communityID         int64
		no, date, vtype         string
		buildingID, householdID sql.NullInt64
		building, room, owner   sql.NullString
		amount                  int64
		summary                 sql.NullString
		masterID                sql.NullInt64
		status                  string
		community               string
		refundKind              string
		bizKind                 string
		createdBy               string
		voidedBy                string
		voidReason              string
	}
	err := db.QueryRow(`SELECT v.id, v.community_id, v.no, v.date, v.type, v.building_id, b.name, v.household_id,
		h.room_no, h.owner, v.amount, v.summary, v.master_id, v.status, c.name, v.refund_kind, v.biz_kind,
		v.created_by, v.voided_by, v.void_reason
	FROM vouchers v JOIN communities c ON v.community_id=c.id
	LEFT JOIN buildings b ON v.building_id=b.id
	LEFT JOIN households h ON v.household_id=h.id
	WHERE v.id=?`, id).Scan(&v.id, &v.communityID, &v.no, &v.date, &v.vtype, &v.buildingID, &v.building,
		&v.householdID, &v.room, &v.owner, &v.amount, &v.summary, &v.masterID, &v.status, &v.community, &v.refundKind,
		&v.bizKind, &v.createdBy, &v.voidedBy, &v.voidReason)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "凭证不存在"})
		return
	}

	allocations := []gin.H{}
	if v.vtype == "expense" || v.vtype == "interest_alloc" {
		rows, err := db.Query(`SELECT a.no, h.room_no, h.owner, a.amount
			FROM vouchers a JOIN households h ON a.household_id=h.id
			WHERE a.master_id=? AND a.type IN ('allocate','interest_alloc_child') ORDER BY h.room_no`, id)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var no, room string
				var owner sql.NullString
				var amount int64
				rows.Scan(&no, &room, &owner, &amount)
				allocations = append(allocations, gin.H{"no": no, "roomNo": room, "owner": owner.String, "amount": centsToYuan(amount)})
			}
		}
	}

	attachments := []gin.H{}
	rows, err := db.Query(`SELECT id, filename, size, created_at FROM attachments WHERE voucher_id=? ORDER BY id`, id)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var aid int64
			var filename string
			var size int64
			var createdAt string
			rows.Scan(&aid, &filename, &size, &createdAt)
			attachments = append(attachments, gin.H{"id": aid, "filename": filename, "size": size, "createdAt": createdAt})
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"id": v.id, "no": v.no, "date": v.date, "type": v.vtype,
		"community": v.community, "building": v.building.String,
		"roomNo": v.room.String, "owner": v.owner.String,
		"amount": centsToYuan(v.amount), "summary": v.summary.String,
		"status": v.status, "refundKind": v.refundKind, "bizKind": v.bizKind,
		"createdBy": v.createdBy, "voidedBy": v.voidedBy, "voidReason": v.voidReason,
		"allocations": allocations, "attachments": attachments,
	})
}

// ==================== 年末结转（年结锁账） ====================
// 年结 = 锁定整个年度：年内所有有业务的月份必须先月结（无损益的月份自动仅锁账），
// 年结后全年凭证禁止新增/作废/传附件，需先反年结；年结时生成年度财务报表快照。

// GET /api/periods/years  已年结年度
func listFiscalYears(c *gin.Context) {
	rows, err := db.Query(`SELECT year, closed_at FROM fiscal_years ORDER BY year DESC`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var year, closedAt string
		rows.Scan(&year, &closedAt)
		out = append(out, gin.H{"year": year, "closedAt": closedAt})
	}
	c.JSON(http.StatusOK, out)
}

// POST /api/periods/close-year  {year}
func closeFiscalYear(c *gin.Context) {
	var req struct {
		Year string `json:"year" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Year) != 4 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请提供年度，格式 yyyy"})
		return
	}
	if _, err := strconv.Atoi(req.Year); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "年度格式应为 yyyy"})
		return
	}
	if closed, _ := isYearClosed(req.Year); closed {
		c.JSON(http.StatusConflict, gin.H{"error": req.Year + " 年度已经年结"})
		return
	}
	tx, err := db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer tx.Rollback()
	// 逐月结转：清除旧结转后按最新账目重新生成（幂等）
	transferred := []string{}
	for m := 1; m <= 12; m++ {
		month := fmt.Sprintf("%s-%02d", req.Year, m)
		var pl int
		tx.QueryRow(`SELECT COUNT(*) FROM gl_entries e
			JOIN gl_vouchers v ON e.voucher_id=v.id AND v.status='normal' AND substr(v.date,1,7)=?
			WHERE substr(e.subject_code,1,1) IN ('4','5')`, month).Scan(&pl)
		if pl == 0 {
			continue
		}
		if err := closeMonthTx(tx, month, c.GetString("authUser")); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": month + " 结转失败：" + err.Error()})
			return
		}
		transferred = append(transferred, month)
	}
	if _, err := tx.Exec(`INSERT INTO fiscal_years(year, closed_at) VALUES(?,?)`,
		req.Year, time.Now().Format(time.RFC3339)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	resp := gin.H{"ok": true, "year": req.Year, "transferredMonths": strings.Join(transferred, "、")}
	if name, err := generateAnnualStatementsSnapshot(req.Year); err != nil {
		resp["statementReportError"] = "年度财务报表快照生成失败：" + err.Error()
	} else {
		resp["statementReport"] = name
	}
	c.JSON(http.StatusOK, resp)
}

// POST /api/periods/reopen-year  {year}  反年度结转：清除全年结转凭证与结转记录
func reopenFiscalYear(c *gin.Context) {
	var req struct {
		Year string `json:"year" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请提供年度"})
		return
	}
	tx, err := db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM gl_entries WHERE voucher_id IN
		(SELECT id FROM gl_vouchers WHERE kind='closing' AND substr(month,1,4)=?)`, req.Year); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if _, err := tx.Exec(`DELETE FROM gl_vouchers WHERE kind='closing' AND substr(month,1,4)=?`, req.Year); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if _, err := tx.Exec(`DELETE FROM periods WHERE substr(month,1,4)=?`, req.Year); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if _, err := tx.Exec(`DELETE FROM fiscal_years WHERE year=?`, req.Year); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "year": req.Year})
}
