package main

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// POST /api/bank/import  {rows: [{date, amount, summary}]}
func bankImport(c *gin.Context) {
	var req struct {
		Rows []struct {
			Date    string  `json:"date" binding:"required"`
			Amount  float64 `json:"amount" binding:"required"`
			Summary string  `json:"summary"`
		} `json:"rows" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "rows 不能为空（date/amount/summary）"})
		return
	}
	tx, err := db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer tx.Rollback()
	now := time.Now().Format(time.RFC3339)
	inserted, skipped := 0, 0
	errs := []string{}
	for i, r := range req.Rows {
		if _, err := time.Parse("2006-01-02", r.Date); err != nil {
			errs = append(errs, strconv.Itoa(i+2)+" 行：日期格式错误（应为 yyyy-MM-dd）")
			skipped++
			continue
		}
		if r.Amount == 0 {
			errs = append(errs, strconv.Itoa(i+2)+" 行：金额为 0，已跳过")
			skipped++
			continue
		}
		if _, err := tx.Exec(`INSERT INTO bank_txns(date, amount, summary, status, created_at) VALUES(?,?,?,'unmatched',?)`,
			r.Date, yuanToCents(r.Amount), r.Summary, now); err != nil {
			errs = append(errs, strconv.Itoa(i+2)+" 行："+err.Error())
			skipped++
			continue
		}
		inserted++
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if len(errs) > 20 {
		errs = errs[:20]
	}
	c.JSON(http.StatusOK, gin.H{"inserted": inserted, "skipped": skipped, "errors": errs})
}

// GET /api/bank/txns?status=
func bankList(c *gin.Context) {
	status := c.Query("status")
	sqlStr := `SELECT id, date, amount, summary, status, matched_voucher_id FROM bank_txns WHERE 1=1`
	args := []interface{}{}
	if status != "" {
		sqlStr += ` AND status=?`
		args = append(args, status)
	}
	sqlStr += ` ORDER BY date DESC, id DESC LIMIT 1000`
	rows, err := db.Query(sqlStr, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id int64
		var date, status string
		var amount int64
		var summary sql.NullString
		var matched sql.NullInt64
		rows.Scan(&id, &date, &amount, &summary, &status, &matched)
		out = append(out, gin.H{
			"id": id, "date": date, "amount": centsToYuan(amount), "summary": summary.String,
			"status": status, "matchedVoucherId": matched.Int64,
		})
	}
	c.JSON(http.StatusOK, out)
}

// GET /api/bank/candidates?date=&amount=  同金额未匹配凭证（供人工挑选）
func bankCandidates(c *gin.Context) {
	date := c.Query("date")
	amountYuan, err := strconv.ParseFloat(c.Query("amount"), 64)
	if err != nil || date == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "date 和 amount 必填"})
		return
	}
	rows, err := db.Query(`SELECT v.id, v.no, v.date, v.type, v.amount, c.name, b.name, h.room_no
	FROM vouchers v
	JOIN communities c ON v.community_id=c.id
	LEFT JOIN buildings b ON v.building_id=b.id
	LEFT JOIN households h ON v.household_id=h.id
	WHERE v.status='normal' AND v.type IN ('income','expense','interest','refund','fund_income','cash','bond')
	  AND v.id NOT IN (SELECT matched_voucher_id FROM bank_txns WHERE matched_voucher_id IS NOT NULL)
	  AND v.amount = ?
	ORDER BY ABS(julianday(v.date) - julianday(?))
	LIMIT 50`, yuanToCents(amountYuan), date)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id int64
		var no, date2, vtype, community string
		var building, room sql.NullString
		var amountCents int64
		rows.Scan(&id, &no, &date2, &vtype, &amountCents, &community, &building, &room)
		out = append(out, gin.H{
			"id": id, "no": no, "date": date2, "type": vtype, "amount": centsToYuan(amountCents),
			"community": community, "building": building.String, "roomNo": room.String,
		})
	}
	c.JSON(http.StatusOK, out)
}

// POST /api/bank/txns/:id/match  {voucherId}
func bankMatch(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效 id"})
		return
	}
	var req struct {
		VoucherID int64 `json:"voucherId" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "voucherId 必填"})
		return
	}
	var status string
	var vdate string
	var vamount int64
	if err := db.QueryRow(`SELECT status, date, amount FROM bank_txns WHERE id=?`, id).Scan(&status, &vdate, &vamount); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "银行流水不存在"})
		return
	}
	if status == "matched" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该流水已匹配"})
		return
	}
	var vstatus string
	var vtype string
	if err := db.QueryRow(`SELECT status, type FROM vouchers WHERE id=?`, req.VoucherID).Scan(&vstatus, &vtype); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "凭证不存在"})
		return
	}
	if vstatus != "normal" || vtype == "allocate" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "只能匹配有效的收入/支出/利息主凭证"})
		return
	}
	if _, err := db.Exec(`UPDATE bank_txns SET status='matched', matched_voucher_id=? WHERE id=?`, req.VoucherID, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// POST /api/bank/txns/:id/ignore
func bankIgnore(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效 id"})
		return
	}
	res, err := db.Exec(`UPDATE bank_txns SET status='ignored' WHERE id=? AND status='unmatched'`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "仅未匹配流水可标记忽略"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// GET /api/reports/households?communityId=  分户余额表（含楼洞名与续筹红线提示）
func reportHouseholds(c *gin.Context) {
	cid, err := strconv.ParseInt(c.Query("communityId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "communityId 必填"})
		return
	}
	rows, err := db.Query(`SELECT b.name, h.room_no, h.owner, h.area, h.opening_balance,
		h.opening_balance + `+replaceAlias(householdDeltaExpr, "h")+`,
		`+firstPaymentExpr+`
	FROM households h JOIN buildings b ON h.building_id=b.id JOIN communities c ON b.community_id=c.id
	WHERE b.community_id=? ORDER BY b.name, h.room_no`, cid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var building, room string
		var owner sql.NullString
		var area float64
		var opening, balance, firstPayment int64
		rows.Scan(&building, &room, &owner, &area, &opening, &balance, &firstPayment)
		out = append(out, gin.H{
			"building": building, "roomNo": room, "owner": owner.String,
			"area": area, "openingBalance": centsToYuan(opening), "balance": centsToYuan(balance),
			"firstPayment": centsToYuan(firstPayment), "belowThreshold": belowThreshold(balance, firstPayment),
		})
	}
	c.JSON(http.StatusOK, out)
}
