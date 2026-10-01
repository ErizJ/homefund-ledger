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

// ---------- 小区 / 楼洞 ----------

func listCommunities(c *gin.Context) {
	rows, err := db.Query(`SELECT id, name, fund_type FROM communities ORDER BY name`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id int64
		var name, fundType string
		rows.Scan(&id, &name, &fundType)
		out = append(out, gin.H{"id": id, "name": name, "fundType": fundType})
	}
	c.JSON(http.StatusOK, out)
}

func createCommunity(c *gin.Context) {
	var req struct {
		Name     string `json:"name" binding:"required"`
		FundType string `json:"fundType"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "小区名称不能为空"})
		return
	}
	if req.FundType != "public" {
		req.FundType = "commercial"
	}
	res, err := db.Exec(`INSERT INTO communities(name, fund_type) VALUES(?,?)`, req.Name, req.FundType)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "小区「" + req.Name + "」已存在"})
		return
	}
	id, _ := res.LastInsertId()
	c.JSON(http.StatusOK, gin.H{"id": id, "name": req.Name, "fundType": req.FundType})
}

func listBuildings(c *gin.Context) {
	cidStr := c.Query("communityId")
	if cidStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "communityId 必填"})
		return
	}
	rows, err := db.Query(`SELECT id, community_id, name FROM buildings WHERE community_id = ? ORDER BY name`, cidStr)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id, communityID int64
		var name string
		rows.Scan(&id, &communityID, &name)
		out = append(out, gin.H{"id": id, "communityId": communityID, "name": name})
	}
	c.JSON(http.StatusOK, out)
}

func createBuilding(c *gin.Context) {
	var req struct {
		CommunityID int64  `json:"communityId" binding:"required"`
		Name        string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误：communityId 和 name 必填"})
		return
	}
	var exists int
	db.QueryRow(`SELECT COUNT(*) FROM communities WHERE id=?`, req.CommunityID).Scan(&exists)
	if exists == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "小区不存在"})
		return
	}
	res, err := db.Exec(`INSERT INTO buildings(community_id, name) VALUES(?,?)`, req.CommunityID, req.Name)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "该楼洞已存在"})
		return
	}
	id, _ := res.LastInsertId()
	c.JSON(http.StatusOK, gin.H{"id": id})
}

// ---------- 住户 ----------

func listHouseholds(c *gin.Context) {
	communityID := c.Query("communityId")
	buildingID := c.Query("buildingId")
	keyword := c.Query("keyword")
	sqlStr := `SELECT h.id, b.community_id, c.name AS community, b.name AS building,
		h.room_no, h.owner, h.area, h.opening_balance,
		(SELECT COUNT(*) FROM vouchers v WHERE v.household_id = h.id) AS has_voucher,
		h.opening_balance + ` + replaceAlias(householdDeltaExpr, "h") + ` AS balance
	FROM households h
	JOIN buildings b ON h.building_id = b.id
	JOIN communities c ON b.community_id = c.id WHERE 1=1`
	args := []interface{}{}
	if buildingID != "" {
		sqlStr += ` AND h.building_id = ?`
		args = append(args, buildingID)
	} else if communityID != "" {
		sqlStr += ` AND b.community_id = ?`
		args = append(args, communityID)
	}
	if keyword != "" {
		kw := "%" + keyword + "%"
		sqlStr += ` AND (h.room_no LIKE ? OR h.owner LIKE ? OR c.name LIKE ? OR b.name LIKE ?)`
		args = append(args, kw, kw, kw, kw)
	}
	sqlStr += ` ORDER BY c.name, b.name, h.room_no LIMIT 5000`
	rows, err := db.Query(sqlStr, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id, communityIDv int64
		var community, building, room string
		var owner string
		var area float64
		var opening, balance int64
		var hasVoucher int
		rows.Scan(&id, &communityIDv, &community, &building, &room, &owner, &area, &opening, &hasVoucher, &balance)
		out = append(out, gin.H{
			"id": id, "communityId": communityIDv, "community": community,
			"building": building, "roomNo": room, "owner": owner,
			"area": area, "openingBalance": centsToYuan(opening), "hasVoucher": hasVoucher,
			"balance": centsToYuan(balance),
		})
	}
	c.JSON(http.StatusOK, out)
}

func deleteHousehold(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效 id"})
		return
	}
	var cnt int
	db.QueryRow(`SELECT COUNT(*) FROM vouchers WHERE household_id=?`, id).Scan(&cnt)
	if cnt > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该住户已有凭证记录，不能删除"})
		return
	}
	res, err := db.Exec(`DELETE FROM households WHERE id=?`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "住户不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// POST /api/households 手工新增住户
func createHousehold(c *gin.Context) {
	var req struct {
		CommunityID    int64   `json:"communityId" binding:"required"`
		BuildingID     int64   `json:"buildingId" binding:"required"`
		RoomNo         string  `json:"roomNo" binding:"required"`
		Owner          string  `json:"owner"`
		Area           float64 `json:"area"`
		OpeningBalance float64 `json:"openingBalance"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误：" + err.Error()})
		return
	}
	if req.Area <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "建筑面积必须大于 0"})
		return
	}
	// 校验楼洞属于所选小区
	var cnt int
	if err := db.QueryRow(`SELECT COUNT(*) FROM buildings WHERE id=? AND community_id=?`, req.BuildingID, req.CommunityID).Scan(&cnt); err != nil || cnt == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "所选楼洞不属于该小区，请重新选择"})
		return
	}
	// 唯一性
	db.QueryRow(`SELECT COUNT(*) FROM households WHERE building_id=? AND room_no=?`, req.BuildingID, req.RoomNo).Scan(&cnt)
	if cnt > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该楼洞下已存在相同户号"})
		return
	}
	res, err := db.Exec(`INSERT INTO households(building_id, room_no, owner, area, opening_balance) VALUES(?,?,?,?,?)`,
		req.BuildingID, req.RoomNo, req.Owner, req.Area, yuanToCents(req.OpeningBalance))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	c.JSON(http.StatusOK, gin.H{"ok": true, "id": id})
}

// PUT /api/households/:id 编辑住户（户主/面积可随时改；期初余额仅无凭证时可改）
func updateHousehold(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效 id"})
		return
	}
	var req struct {
		Owner          *string  `json:"owner"`
		Area           *float64 `json:"area"`
		OpeningBalance *float64 `json:"openingBalance"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误：" + err.Error()})
		return
	}
	if req.Area != nil && *req.Area <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "建筑面积必须大于 0"})
		return
	}
	if req.OpeningBalance != nil {
		var cnt int
		db.QueryRow(`SELECT COUNT(*) FROM vouchers WHERE household_id=?`, id).Scan(&cnt)
		if cnt > 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "该住户已有账务记录，期初余额不可修改；如需调整请联系管理员处理凭证"})
			return
		}
	}
	if req.Owner != nil {
		if _, err := db.Exec(`UPDATE households SET owner=? WHERE id=?`, *req.Owner, id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	if req.Area != nil {
		if _, err := db.Exec(`UPDATE households SET area=? WHERE id=?`, *req.Area, id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	if req.OpeningBalance != nil {
		if _, err := db.Exec(`UPDATE households SET opening_balance=? WHERE id=?`, yuanToCents(*req.OpeningBalance), id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// GET /api/households/:id/statement 住户基本信息 + 当前余额 + 收支流水（余额由期初逐笔累计，与四级账同源）
func householdStatement(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效 id"})
		return
	}
	var community, building, room, owner string
	var area float64
	var opening, balance int64
	err = db.QueryRow(`SELECT c.name, b.name, h.room_no, h.owner, h.area, h.opening_balance,
		h.opening_balance + `+replaceAlias(householdDeltaExpr, "h")+`
	FROM households h JOIN buildings b ON h.building_id=b.id JOIN communities c ON b.community_id=c.id
	WHERE h.id=?`, id).Scan(&community, &building, &room, &owner, &area, &opening, &balance)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "住户不存在"})
		return
	}
	rows, err := db.Query(`SELECT v.date, v.type, v.amount, v.summary, v.no, v.status
	FROM vouchers v WHERE v.household_id=? ORDER BY v.date, v.id`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	lines := []gin.H{}
	running := opening
	for rows.Next() {
		var date, vtype, summary, no, status string
		var amount int64
		rows.Scan(&date, &vtype, &amount, &summary, &no, &status)
		if status != "normal" {
			// 作废凭证留痕展示，但不计入逐笔余额
			lines = append(lines, gin.H{
				"date": date, "type": vtype, "amount": centsToYuan(amount),
				"summary": summary, "no": no, "status": status,
				"balance": centsToYuan(running),
			})
			continue
		}
		if vtype == "income" {
			running += amount
		} else if vtype == "allocate" {
			running -= amount
		} else {
			continue // interest 等不落户账的流水不进入住户对账单
		}
		lines = append(lines, gin.H{
			"date": date, "type": vtype, "amount": centsToYuan(amount),
			"summary": summary, "no": no, "status": status,
			"balance": centsToYuan(running),
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"community": community, "building": building, "roomNo": room, "owner": owner,
		"area": area, "openingBalance": centsToYuan(opening), "balance": centsToYuan(balance), "lines": lines,
	})
}

// Excel/CSV 导入：前端解析后以 JSON 行提交
type importRow struct {
	Community string  `json:"community"`
	Building  string  `json:"building"`
	Room      string  `json:"room"`
	Owner     string  `json:"owner"`
	Area      float64 `json:"area"`
	Opening   float64 `json:"opening"`
}

func importHouseholds(c *gin.Context) {
	var req struct {
		Rows []importRow `json:"rows" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "rows 不能为空"})
		return
	}
	tx, err := db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer tx.Rollback()

	inserted, updated, skipped := 0, 0, 0
	errs := []string{}
	for i, r := range req.Rows {
		lineNo := i + 2
		if r.Community == "" || r.Building == "" || r.Room == "" {
			errs = append(errs, strconv.Itoa(lineNo)+" 行：小区/楼洞/户号不能为空")
			skipped++
			continue
		}
		if r.Area < 0 || r.Opening < 0 {
			errs = append(errs, strconv.Itoa(lineNo)+" 行：面积或期初余额不能为负")
			skipped++
			continue
		}
		var cid int64
		err := tx.QueryRow(`SELECT id FROM communities WHERE name=?`, r.Community).Scan(&cid)
		if err != nil {
			res, e := tx.Exec(`INSERT INTO communities(name) VALUES(?)`, r.Community)
			if e != nil {
				errs = append(errs, strconv.Itoa(lineNo)+" 行："+e.Error())
				skipped++
				continue
			}
			cid, _ = res.LastInsertId()
		}
		var bid int64
		err = tx.QueryRow(`SELECT id FROM buildings WHERE community_id=? AND name=?`, cid, r.Building).Scan(&bid)
		if err != nil {
			res, e := tx.Exec(`INSERT INTO buildings(community_id, name) VALUES(?,?)`, cid, r.Building)
			if e != nil {
				errs = append(errs, strconv.Itoa(lineNo)+" 行："+e.Error())
				skipped++
				continue
			}
			bid, _ = res.LastInsertId()
		}
		var hid int64
		err = tx.QueryRow(`SELECT id FROM households WHERE building_id=? AND room_no=?`, bid, r.Room).Scan(&hid)
		if err == nil {
			if _, e := tx.Exec(`UPDATE households SET owner=?, area=? WHERE id=?`, r.Owner, r.Area, hid); e != nil {
				errs = append(errs, strconv.Itoa(lineNo)+" 行："+e.Error())
				skipped++
				continue
			}
			updated++
		} else {
			if _, e := tx.Exec(`INSERT INTO households(building_id, room_no, owner, area, opening_balance) VALUES(?,?,?,?,?)`,
				bid, r.Room, r.Owner, r.Area, yuanToCents(r.Opening)); e != nil {
				errs = append(errs, strconv.Itoa(lineNo)+" 行："+e.Error())
				skipped++
				continue
			}
			inserted++
		}
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if len(errs) > 20 {
		errs = errs[:20]
	}
	c.JSON(http.StatusOK, gin.H{"inserted": inserted, "updated": updated, "skipped": skipped, "errors": errs})
}

// ---------- 凭证 ----------

func listVouchers(c *gin.Context) {
	month := c.Query("month")
	vType := c.Query("type")
	limit := 500
	if l, e := strconv.Atoi(c.DefaultQuery("limit", "500")); e == nil && l > 0 {
		limit = l
	}
	sqlStr := `SELECT v.id, v.no, v.date, v.type, v.community_id, c.name AS community,
		v.building_id, b.name AS building, v.household_id, h.room_no, h.owner,
		v.amount, v.summary, v.master_id, v.status
	FROM vouchers v
	JOIN communities c ON v.community_id = c.id
	LEFT JOIN buildings b ON v.building_id = b.id
	LEFT JOIN households h ON v.household_id = h.id
	WHERE 1=1`
	args := []interface{}{}
	if month != "" {
		sqlStr += ` AND substr(v.date,1,7) = ?`
		args = append(args, month)
	}
	if vType != "" {
		sqlStr += ` AND v.type = ?`
		args = append(args, vType)
	}
	sqlStr += ` ORDER BY v.date DESC, v.id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := db.Query(sqlStr, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id, communityID int64
		var no, date, vtype, community string
		var buildingID, householdID, masterID sql.NullInt64
		var building, room, owner, summary sql.NullString
		var amount int64
		var status string
		if err := rows.Scan(&id, &no, &date, &vtype, &communityID, &community,
			&buildingID, &building, &householdID, &room, &owner,
			&amount, &summary, &masterID, &status); err != nil {
			continue
		}
		out = append(out, gin.H{
			"id": id, "no": no, "date": date, "type": vtype,
			"communityId": communityID, "community": community,
			"buildingId": buildingID.Int64, "building": building.String,
			"householdId": householdID.Int64, "roomNo": room.String, "owner": owner.String,
			"amount": centsToYuan(amount), "summary": summary.String,
			"masterId": masterID.Int64, "status": status,
		})
	}
	c.JSON(http.StatusOK, out)
}

type createVoucherReq struct {
	Type        string  `json:"type" binding:"required"`
	Date        string  `json:"date" binding:"required"`
	CommunityID int64   `json:"communityId" binding:"required"`
	BuildingID  int64   `json:"buildingId"`  // 0 表示未选；expense 且 scope=community 时为全体楼洞
	Scope       string  `json:"scope"`       // expense 用：building | community
	HouseholdID int64   `json:"householdId"` // income 用
	Amount      float64 `json:"amount" binding:"required"`
	Summary     string  `json:"summary"`
	Category    string  `json:"category"` // expense 用：engineering|supervision|survey|other → 5001 明细科目
}

func nextVoucherNoTx(tx *sql.Tx, date string) (string, error) {
	var cnt int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM vouchers WHERE date=?`, date).Scan(&cnt); err != nil {
		return "", err
	}
	return fmt.Sprintf("PZ%s-%04d", strings.ReplaceAll(date, "-", ""), cnt+1), nil
}

// 按建筑面积分摊（单位：分），尾差给最后一户
func splitByArea(amountCents int64, areas []float64) []int64 {
	n := len(areas)
	if n == 0 {
		return nil
	}
	res := make([]int64, n)
	var totalArea float64
	for _, a := range areas {
		totalArea += a
	}
	if totalArea <= 0 {
		each := amountCents / int64(n)
		for i := range res {
			res[i] = each
		}
		res[n-1] = amountCents - each*int64(n-1)
		return res
	}
	var allocated int64
	for i := 0; i < n-1; i++ {
		v := int64(float64(amountCents) * areas[i] / totalArea)
		res[i] = v
		allocated += v
	}
	res[n-1] = amountCents - allocated
	return res
}

func createVoucher(c *gin.Context) {
	var req createVoucherReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误：" + err.Error()})
		return
	}
	if req.Amount <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "金额必须大于 0"})
		return
	}
	amountCents := yuanToCents(req.Amount)
	if amountCents <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "金额至少 0.01 元"})
		return
	}
	if _, err := time.Parse("2006-01-02", req.Date); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "日期格式应为 yyyy-MM-dd"})
		return
	}
	if closed, err := isMonthClosed(req.Date[:7]); err == nil && closed {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该月份已月结锁账，如需记账请先反结转"})
		return
	}

	tx, err := db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer tx.Rollback()
	now := time.Now().Format(time.RFC3339)
	no, err := nextVoucherNoTx(tx, req.Date)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	switch req.Type {
	case "income":
		if req.BuildingID <= 0 || req.HouseholdID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "缴纳收入必须选择楼洞和户室"})
			return
		}
		// 校验户室归属：户室必须属于所选小区和楼洞，防止 API 直调造成跨小区错账
		var hhCnt int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM households h JOIN buildings b ON h.building_id=b.id
			WHERE h.id=? AND h.building_id=? AND b.community_id=?`,
			req.HouseholdID, req.BuildingID, req.CommunityID).Scan(&hhCnt); err != nil || hhCnt == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "所选户室不属于该小区/楼洞，请重新选择"})
			return
		}
		if _, err := tx.Exec(`INSERT INTO vouchers(no,date,type,community_id,building_id,household_id,amount,summary,status,created_at)
			VALUES(?,?,?,?,?,?,?,?, 'normal', ?)`,
			no, req.Date, "income", req.CommunityID, req.BuildingID, req.HouseholdID, amountCents, req.Summary, now); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		var bizID int64
		tx.QueryRow(`SELECT last_insert_rowid()`).Scan(&bizID)
		if err := generateBusinessGL(tx, "income", req.Date, req.CommunityID, amountCents, "", req.Summary, bizID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "生成财务凭证失败：" + err.Error()})
			return
		}
		if err := tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"no": no, "id": bizID})
		return

	case "interest":
		if _, err := tx.Exec(`INSERT INTO vouchers(no,date,type,community_id,building_id,household_id,amount,summary,status,created_at)
			VALUES(?,?,?,?,NULL,NULL,?,?,'normal',?)`,
			no, req.Date, "interest", req.CommunityID, amountCents, req.Summary, now); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		var bizID int64
		tx.QueryRow(`SELECT last_insert_rowid()`).Scan(&bizID)
		if err := generateBusinessGL(tx, "interest", req.Date, req.CommunityID, amountCents, "", req.Summary, bizID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "生成财务凭证失败：" + err.Error()})
			return
		}
		if err := tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"no": no, "id": bizID})
		return

	case "expense":
		// 确定分摊目标住户
		var targets []struct {
			id          int64
			buildingID  int64
			area        float64
		}
		var query string
		var args []interface{}
		if req.Scope == "community" || req.BuildingID <= 0 {
			query = `SELECT h.id, h.building_id, h.area FROM households h JOIN buildings b ON h.building_id=b.id WHERE b.community_id=? ORDER BY h.id`
			args = []interface{}{req.CommunityID}
		} else {
			query = `SELECT h.id, h.building_id, h.area FROM households h WHERE h.building_id=? ORDER BY h.id`
			args = []interface{}{req.BuildingID}
		}
		rows, err := tx.Query(query, args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		for rows.Next() {
			var t struct {
				id         int64
				buildingID int64
				area       float64
			}
			rows.Scan(&t.id, &t.buildingID, &t.area)
			targets = append(targets, t)
		}
		rows.Close()
		if len(targets) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "该范围内没有住户，无法分摊"})
			return
		}
		var masterBuilding interface{}
		if req.BuildingID > 0 {
			masterBuilding = req.BuildingID
		}
		if _, err := tx.Exec(`INSERT INTO vouchers(no,date,type,community_id,building_id,household_id,amount,summary,expense_category,status,created_at)
			VALUES(?,?,?,?,?,NULL,?,?,?, 'normal', ?)`,
			no, req.Date, "expense", req.CommunityID, masterBuilding, amountCents, req.Summary, req.Category, now); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		var masterID int64
		tx.QueryRow(`SELECT last_insert_rowid()`).Scan(&masterID)
		if err := generateBusinessGL(tx, "expense", req.Date, req.CommunityID, amountCents, req.Category, req.Summary, masterID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "生成财务凭证失败：" + err.Error()})
			return
		}

		areas := make([]float64, len(targets))
		for i, t := range targets {
			areas[i] = t.area
		}
		shares := splitByArea(amountCents, areas)
		summary := req.Summary
		if summary == "" {
			summary = "维修支出"
		}
		for i, t := range targets {
			if shares[i] == 0 {
				continue
			}
			childNo := fmt.Sprintf("%s-A%03d", no, i+1)
			childSummary := "分摊：" + summary + "（主凭证 " + no + "）"
			if _, err := tx.Exec(`INSERT INTO vouchers(no,date,type,community_id,building_id,household_id,amount,summary,master_id,status,created_at)
				VALUES(?,?,?,?,?,?,?,?,?,'normal',?)`,
				childNo, req.Date, "allocate", req.CommunityID, t.buildingID, t.id,
				shares[i], childSummary, masterID, now); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "生成分摊凭证失败：" + err.Error()})
				return
			}
		}
		if err := tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"no": no, "masterId": masterID, "allocations": len(targets)})
		return

	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "未知的凭证类型"})
		return
	}
}

func voidVoucher(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效 id"})
		return
	}
	var vtype, status, vdate string
	err = db.QueryRow(`SELECT type, status, date FROM vouchers WHERE id=?`, id).Scan(&vtype, &status, &vdate)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "凭证不存在"})
		return
	}
	if status != "normal" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该凭证已是作废状态"})
		return
	}
	if closed, err := isMonthClosed(vdate[:7]); err == nil && closed {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该凭证所在月份已月结锁账，请先反结转再作废"})
		return
	}
	tx, err := db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE vouchers SET status='voided', void_of=? WHERE id=?`, id, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	children := 0
	if vtype == "expense" {
		res, err := tx.Exec(`UPDATE vouchers SET status='voided', void_of=? WHERE master_id=? AND status='normal'`, id, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if n, e := res.RowsAffected(); e == nil {
			children = int(n)
		}
	}
	// 级联作废对应的财务记账凭证（income/interest/expense 均产生 GL 凭证）
	voidedGL := 0
	if res, err := tx.Exec(`UPDATE gl_vouchers SET status='voided' WHERE source_type='voucher' AND source_id=? AND status='normal'`, id); err == nil {
		if n, e := res.RowsAffected(); e == nil {
			voidedGL = int(n)
		}
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "voidedChildren": children, "voidedGL": voidedGL})
}
