package app

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// 户账变动口径：缴存 + 利息分配 - 维修分摊 - 返还/退返
const householdDeltaExpr = `IFNULL((SELECT SUM(CASE v.type WHEN 'income' THEN v.amount WHEN 'interest_alloc_child' THEN v.amount WHEN 'allocate' THEN -v.amount WHEN 'refund' THEN -v.amount ELSE 0 END)
		FROM vouchers v WHERE v.household_id = {alias}.id AND v.status = 'normal'), 0)`

// 小区公共账变动口径：利息收入 + 其他收入（经营/处置/其他） - 已分配收益
const publicDeltaExpr = `IFNULL((SELECT SUM(CASE v.type WHEN 'interest' THEN v.amount WHEN 'fund_income' THEN v.amount WHEN 'interest_alloc' THEN -v.amount ELSE 0 END)
		FROM vouchers v WHERE v.community_id = {alias}.id AND v.status = 'normal'), 0)`

// GET /api/ledger/communities  一级总账 + 二级小区
func ledgerCommunities(c *gin.Context) {
	rows, err := db.Query(`SELECT c.id, c.name, c.public_opening, c.first_rate,
		(SELECT COUNT(*) FROM households h JOIN buildings b ON h.building_id=b.id WHERE b.community_id=c.id) AS hh_count,
		IFNULL((SELECT SUM(h.opening_balance) FROM households h JOIN buildings b ON h.building_id=b.id WHERE b.community_id=c.id),0)
			+ IFNULL((SELECT SUM(CASE v.type WHEN 'income' THEN v.amount WHEN 'interest_alloc_child' THEN v.amount WHEN 'allocate' THEN -v.amount WHEN 'refund' THEN -v.amount ELSE 0 END)
				FROM vouchers v JOIN households h2 ON v.household_id=h2.id JOIN buildings b2 ON h2.building_id=b2.id
				WHERE b2.community_id=c.id AND v.status='normal'),0) AS households_balance,
		c.public_opening + ` + replaceAlias(publicDeltaExpr, "c") + ` AS public_balance
	FROM communities c ORDER BY c.name`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id, hhCount int64
		var name string
		var householdsBal, publicBal, publicOpening int64
		var firstRate float64
		rows.Scan(&id, &name, &publicOpening, &firstRate, &hhCount, &householdsBal, &publicBal)
		out = append(out, gin.H{
			"id": id, "name": name, "householdCount": hhCount,
			"publicOpening": centsToYuan(publicOpening), "firstRate": firstRate,
			"householdsBalance": centsToYuan(householdsBal), "publicBalance": centsToYuan(publicBal),
			"balance": centsToYuan(householdsBal + publicBal),
		})
	}
	c.JSON(http.StatusOK, out)
}

// 把 householdDeltaExpr 中的 {alias} 占位替换为指定表别名
func replaceAlias(expr, alias string) string {
	out := ""
	for i := 0; i < len(expr); {
		if i+7 <= len(expr) && expr[i:i+7] == "{alias}" {
			out += alias
			i += 7
			continue
		}
		out += string(expr[i])
		i++
	}
	return out
}

// GET /api/ledger/buildings?communityId=  三级楼洞
func ledgerBuildings(c *gin.Context) {
	cid, err := strconv.ParseInt(c.Query("communityId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "communityId 必填"})
		return
	}
	rows, err := db.Query(`SELECT b.id, b.name,
		COUNT(h.id),
		IFNULL(SUM(h.opening_balance + `+replaceAlias(householdDeltaExpr, "h")+`),0)
	FROM buildings b
	LEFT JOIN households h ON h.building_id = b.id
	WHERE b.community_id = ?
	GROUP BY b.id, b.name ORDER BY b.name`, cid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id, hhCount int64
		var name string
		var balance int64
		rows.Scan(&id, &name, &hhCount, &balance)
		out = append(out, gin.H{"id": id, "name": name, "householdCount": hhCount, "balance": centsToYuan(balance)})
	}
	c.JSON(http.StatusOK, out)
}

// GET /api/ledger/households?buildingId=  四级住户
func ledgerHouseholds(c *gin.Context) {
	bid, err := strconv.ParseInt(c.Query("buildingId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "buildingId 必填"})
		return
	}
	rows, err := db.Query(`SELECT h.id, h.room_no, h.owner, h.area, h.opening_balance,
		h.opening_balance + `+replaceAlias(householdDeltaExpr, "h")+`,
		`+firstPaymentExpr+`
	FROM households h JOIN buildings b ON h.building_id=b.id JOIN communities c ON b.community_id=c.id
	WHERE h.building_id = ? ORDER BY h.room_no`, bid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id int64
		var room string
		var owner string
		var area float64
		var opening, balance, firstPayment int64
		rows.Scan(&id, &room, &owner, &area, &opening, &balance, &firstPayment)
		out = append(out, gin.H{
			"id": id, "roomNo": room, "owner": owner,
			"area": area, "openingBalance": centsToYuan(opening), "balance": centsToYuan(balance),
			"firstPayment": centsToYuan(firstPayment), "belowThreshold": belowThreshold(balance, firstPayment),
		})
	}
	c.JSON(http.StatusOK, out)
}

// GET /api/stats/dashboard
func statsDashboard(c *gin.Context) {
	var households, communities int64
	var totalBalance int64
	db.QueryRow(`SELECT COUNT(*) FROM households`).Scan(&households)
	db.QueryRow(`SELECT COUNT(*) FROM communities`).Scan(&communities)
	// 总余额 = 户账合计（期初+缴存+利息分配-分摊-返还）+ 公共账（公共账期初+利息+其他收入-已分配收益）
	db.QueryRow(`SELECT IFNULL(SUM(x.opening + x.delta),0)
		+ IFNULL((SELECT SUM(c.public_opening) FROM communities c),0)
		+ IFNULL((SELECT SUM(CASE v.type WHEN 'interest' THEN v.amount WHEN 'fund_income' THEN v.amount WHEN 'interest_alloc' THEN -v.amount ELSE 0 END)
			FROM vouchers v WHERE v.status='normal'),0)
	FROM (
		SELECT h.opening_balance AS opening, ` + replaceAlias(householdDeltaExpr, "h") + ` AS delta
		FROM households h
	) x`).Scan(&totalBalance)
	today := nowDate()
	month := today[:7]
	// 支出只统计 expense 主凭证：分摊子凭证（allocate）与主凭证是同一笔钱，合计会双计；
	// 收入 = 缴存 - 退返，支出 = 维修支出 + 灭失返还
	var todayIncome, todayExpense, monthIncome, monthExpense int64
	db.QueryRow(`SELECT
		IFNULL(SUM(CASE WHEN type='income' THEN amount WHEN type='refund' AND refund_kind='return' THEN -amount END),0),
		IFNULL(SUM(CASE WHEN type='expense' THEN amount WHEN type='refund' AND refund_kind='destroy' THEN amount END),0)
	FROM vouchers WHERE status='normal' AND date=?`, today).Scan(&todayIncome, &todayExpense)
	db.QueryRow(`SELECT
		IFNULL(SUM(CASE WHEN type='income' THEN amount WHEN type='refund' AND refund_kind='return' THEN -amount END),0),
		IFNULL(SUM(CASE WHEN type='expense' THEN amount WHEN type='refund' AND refund_kind='destroy' THEN amount END),0)
	FROM vouchers WHERE status='normal' AND substr(date,1,7)=?`, month).Scan(&monthIncome, &monthExpense)
	c.JSON(http.StatusOK, gin.H{
		"households": households, "communities": communities,
		"totalBalance": centsToYuan(totalBalance),
		"todayIncome":  centsToYuan(todayIncome), "todayExpense": centsToYuan(todayExpense),
		"monthIncome": centsToYuan(monthIncome), "monthExpense": centsToYuan(monthExpense),
		"today": today,
	})
}

func nowDate() string { return time.Now().Format("2006-01-02") }

// GET /api/summary/daily?limit=30
func summaryDaily(c *gin.Context) {
	limit := 30
	if l, e := strconv.Atoi(c.DefaultQuery("limit", "30")); e == nil && l > 0 {
		limit = l
	}
	summaryBy(c, `date`, limit)
}

// GET /api/summary/monthly?limit=24
func summaryMonthly(c *gin.Context) {
	limit := 24
	if l, e := strconv.Atoi(c.DefaultQuery("limit", "24")); e == nil && l > 0 {
		limit = l
	}
	summaryBy(c, `substr(date,1,7)`, limit)
}

// GET /api/summary/yearly  年度汇总（全部年份）
func summaryYearly(c *gin.Context) {
	summaryBy(c, `substr(date,1,4)`, 1000)
}

// GET /api/reports/community-statement?communityId=&year=  小区对账单
func reportCommunityStatement(c *gin.Context) {
	cid, err := strconv.ParseInt(c.Query("communityId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "communityId 必填"})
		return
	}
	year := c.Query("year")
	if year == "" {
		year = nowDate()[:4]
	}
	if _, err := strconv.Atoi(year); err != nil || len(year) != 4 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "year 格式应为 yyyy"})
		return
	}
	var cname string
	var publicOpening int64
	if err := db.QueryRow(`SELECT name, public_opening FROM communities WHERE id=?`, cid).Scan(&cname, &publicOpening); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "小区不存在"})
		return
	}

	// 期初余额（该年 1 月 1 日前）= 户账（期初建账+缴存+利息分配-分摊-返还）+ 公共账（公共账期初+利息-已分配利息）
	var opening int64
	householdDeltaBefore := `IFNULL((SELECT SUM(CASE v.type WHEN 'income' THEN v.amount WHEN 'interest_alloc_child' THEN v.amount WHEN 'allocate' THEN -v.amount WHEN 'refund' THEN -v.amount ELSE 0 END)
		FROM vouchers v JOIN households h2 ON v.household_id=h2.id JOIN buildings b2 ON h2.building_id=b2.id
		WHERE b2.community_id=? AND v.status='normal' AND v.date < ?),0)`
	publicBefore := `IFNULL((SELECT SUM(CASE v.type WHEN 'interest' THEN v.amount WHEN 'fund_income' THEN v.amount WHEN 'interest_alloc' THEN -v.amount ELSE 0 END)
		FROM vouchers v WHERE v.community_id=? AND v.status='normal' AND v.date < ?),0)`
	db.QueryRow(`SELECT
		IFNULL((SELECT SUM(h.opening_balance) FROM households h JOIN buildings b ON h.building_id=b.id WHERE b.community_id=?),0)
		+ `+householdDeltaBefore+` + `+publicBefore+` + ?`,
		cid, cid, year+"-01-01", cid, year+"-01-01", publicOpening).Scan(&opening)

	// 本年六类发生额
	var yi, ye, ya, yint, yr, yfi int64
	db.QueryRow(`SELECT
		IFNULL(SUM(CASE WHEN type='income' THEN amount END),0),
		IFNULL(SUM(CASE WHEN type='expense' THEN amount END),0),
		IFNULL(SUM(CASE WHEN type='allocate' THEN amount END),0),
		IFNULL(SUM(CASE WHEN type='interest' THEN amount END),0),
		IFNULL(SUM(CASE WHEN type='refund' THEN amount END),0),
		IFNULL(SUM(CASE WHEN type='fund_income' THEN amount END),0)
	FROM vouchers WHERE status='normal' AND community_id=? AND substr(date,1,4)=?`, cid, year).Scan(&yi, &ye, &ya, &yint, &yr, &yfi)

	// 本年逐月发生额
	rows, err := db.Query(`SELECT substr(date,1,7) AS m,
		IFNULL(SUM(CASE WHEN type='income' THEN amount END),0),
		IFNULL(SUM(CASE WHEN type='expense' THEN amount END),0),
		IFNULL(SUM(CASE WHEN type='interest' THEN amount END),0),
		IFNULL(SUM(CASE WHEN type='allocate' THEN amount END),0),
		IFNULL(SUM(CASE WHEN type='refund' THEN amount END),0),
		IFNULL(SUM(CASE WHEN type='interest_alloc' THEN amount END),0),
		IFNULL(SUM(CASE WHEN type='fund_income' THEN amount END),0),
		COUNT(*)
	FROM vouchers WHERE status='normal' AND community_id=? AND substr(date,1,4)=?
	GROUP BY m ORDER BY m`, cid, year)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	months := []gin.H{}
	for rows.Next() {
		var m string
		var income, expense, interest, allocate, refund, interestAlloc, fundIncome int64
		var cnt int
		rows.Scan(&m, &income, &expense, &interest, &allocate, &refund, &interestAlloc, &fundIncome, &cnt)
		months = append(months, gin.H{"month": m, "income": centsToYuan(income), "expense": centsToYuan(expense),
			"interest": centsToYuan(interest), "allocate": centsToYuan(allocate),
			"refund": centsToYuan(refund), "interestAlloc": centsToYuan(interestAlloc),
			"fundIncome": centsToYuan(fundIncome), "count": cnt})
	}

	closing := opening + yi + yint + yfi - ye - yr
	c.JSON(http.StatusOK, gin.H{
		"community": cname, "year": year,
		"opening": centsToYuan(opening), "closing": centsToYuan(closing),
		"income": centsToYuan(yi), "expense": centsToYuan(ye),
		"allocate": centsToYuan(ya), "interest": centsToYuan(yint),
		"refund": centsToYuan(yr), "publicOpening": centsToYuan(publicOpening),
		"fundIncome": centsToYuan(yfi),
		"months":     months,
	})
}

func summaryBy(c *gin.Context, groupExpr string, limit int) {
	rows, err := db.Query(`SELECT `+groupExpr+` AS g,
		IFNULL(SUM(CASE WHEN type='income' THEN amount END),0),
		IFNULL(SUM(CASE WHEN type='expense' THEN amount END),0),
		IFNULL(SUM(CASE WHEN type='interest' THEN amount END),0),
		IFNULL(SUM(CASE WHEN type='allocate' THEN amount END),0),
		IFNULL(SUM(CASE WHEN type='refund' THEN amount END),0),
		IFNULL(SUM(CASE WHEN type='interest_alloc' THEN amount END),0),
		IFNULL(SUM(CASE WHEN type='fund_income' THEN amount END),0),
		COUNT(*)
	FROM vouchers WHERE status='normal' GROUP BY g ORDER BY g DESC LIMIT ?`, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var g string
		var income, expense, interest, allocate, refund, interestAlloc, fundIncome int64
		var cnt int
		rows.Scan(&g, &income, &expense, &interest, &allocate, &refund, &interestAlloc, &fundIncome, &cnt)
		out = append(out, gin.H{
			"group": g, "income": centsToYuan(income), "expense": centsToYuan(expense),
			"interest": centsToYuan(interest), "allocate": centsToYuan(allocate),
			"refund": centsToYuan(refund), "interestAlloc": centsToYuan(interestAlloc),
			"fundIncome": centsToYuan(fundIncome), "count": cnt,
		})
	}
	c.JSON(http.StatusOK, out)
}
