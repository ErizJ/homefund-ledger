package main

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// errNothingToClose 该月没有需要结转的收入/支出发生额
var errNothingToClose = errors.New("该月份没有收入/支出类财务发生额，无需月结")

// closeMonthTx 在事务内执行月结核心逻辑：
// 删除该月旧结转凭证（含手工期末转账，避免双重结转）→ 按科目+项目汇总当月损益 →
// 生成月末结转凭证（收入→净资产/待分配收益，支出→净资产）→ 写入 periods 锁账。
func closeMonthTx(tx *sql.Tx, month string) error {
	// 若该月已做过手工期末转账（gl/transfer），先删除其结转凭证，月结时统一重新生成，避免双重结转
	if _, err := tx.Exec(`DELETE FROM gl_entries WHERE voucher_id IN
		(SELECT id FROM gl_vouchers WHERE kind='closing' AND month=?)`, month); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM gl_vouchers WHERE kind='closing' AND month=?`, month); err != nil {
		return err
	}

	// 汇总本月收入/支出类科目的发生额（按科目+项目分组）
	type agg struct {
		subject  string
		project  sql.NullInt64
		dir      string
		amount   int64
		fundType string
	}
	aggs := []agg{}
	rows, err := tx.Query(`
		SELECT e.subject_code, e.project_id, e.direction, IFNULL(SUM(e.amount),0),
			IFNULL((SELECT fund_type FROM communities cm WHERE cm.id=e.project_id),'commercial')
		FROM gl_entries e
		JOIN gl_vouchers v ON e.voucher_id=v.id AND v.status='normal' AND substr(v.date,1,7)=?
		JOIN gl_subjects s ON e.subject_code=s.code
		WHERE substr(e.subject_code,1,1) IN ('4','5')
		GROUP BY e.subject_code, e.project_id, e.direction`, month)
	if err != nil {
		return err
	}
	for rows.Next() {
		var a agg
		rows.Scan(&a.subject, &a.project, &a.dir, &a.amount, &a.fundType)
		aggs = append(aggs, a)
	}
	rows.Close()
	if len(aggs) == 0 {
		return errNothingToClose
	}

	lastDate := month + "-01"
	if t, e := time.Parse("2006-01", month); e == nil {
		lastDate = t.AddDate(0, 1, -1).Format("2006-01-02")
	}
	fund := func(a agg) string {
		if a.fundType == "public" {
			return "public"
		}
		return "commercial"
	}
	entries := []glEntry{}
	for _, a := range aggs {
		pid := a.project
		if strings.HasPrefix(a.subject, "4") && a.dir == "credit" {
			// 收入结转：借 收入科目 / 贷 净资产（交存→维修资金；利息处置→待分配收益）
			entries = append(entries, glEntry{a.subject, pid, "debit", a.amount})
			slot := "pending"
			if strings.HasPrefix(a.subject, "4001") {
				slot = "netasset"
			}
			entries = append(entries, glEntry{glSlot(fund(a), slot), pid, "credit", a.amount})
		} else if strings.HasPrefix(a.subject, "5") && a.dir == "debit" {
			// 支出结转：借 净资产 / 贷 支出科目
			entries = append(entries, glEntry{glSlot(fund(a), "netasset"), pid, "debit", a.amount})
			entries = append(entries, glEntry{a.subject, pid, "credit", a.amount})
		}
	}
	if _, err := glInsertTx(tx, lastDate, month, "closing", "period", 0, "月末结转 "+month, entries); err != nil {
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
// 月结：锁账 + 自动生成月末结转凭证（收入→净资产；支出→净资产）
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
	var cnt int
	db.QueryRow(`SELECT COUNT(*) FROM vouchers WHERE substr(date,1,7)=? AND status='normal' AND void_of IS NULL`, req.Month).Scan(&cnt)
	if cnt == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该月份没有有效凭证，无需月结"})
		return
	}
	var closedCnt int
	db.QueryRow(`SELECT COUNT(*) FROM periods WHERE month=?`, req.Month).Scan(&closedCnt)
	if closedCnt > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "该月份已经月结"})
		return
	}

	tx, err := db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer tx.Rollback()
	if err := closeMonthTx(tx, req.Month); err != nil {
		if errors.Is(err, errNothingToClose) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// 月结成功后自动生成当月月报表（4 子表 Excel）；失败不影响月结结果
	resp := gin.H{"ok": true, "month": req.Month}
	if name, err := generateMonthlyReport(req.Month); err != nil {
		resp["reportError"] = "月报表生成失败：" + err.Error()
	} else {
		resp["report"] = name
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
		id, communityID          int64
		no, date, vtype          string
		buildingID, householdID  sql.NullInt64
		building, room, owner    sql.NullString
		amount                   int64
		summary                  sql.NullString
		masterID                 sql.NullInt64
		status                   string
		community                string
	}
	err := db.QueryRow(`SELECT v.id, v.community_id, v.no, v.date, v.type, v.building_id, b.name, v.household_id,
		h.room_no, h.owner, v.amount, v.summary, v.master_id, v.status, c.name
	FROM vouchers v JOIN communities c ON v.community_id=c.id
	LEFT JOIN buildings b ON v.building_id=b.id
	LEFT JOIN households h ON v.household_id=h.id
	WHERE v.id=?`, id).Scan(&v.id, &v.communityID, &v.no, &v.date, &v.vtype, &v.buildingID, &v.building,
		&v.householdID, &v.room, &v.owner, &v.amount, &v.summary, &v.masterID, &v.status, &v.community)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "凭证不存在"})
		return
	}

	allocations := []gin.H{}
	if v.vtype == "expense" {
		rows, err := db.Query(`SELECT a.no, h.room_no, h.owner, a.amount
			FROM vouchers a JOIN households h ON a.household_id=h.id
			WHERE a.master_id=? AND a.type='allocate' ORDER BY h.room_no`, id)
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
		"status": v.status, "allocations": allocations, "attachments": attachments,
	})
}
