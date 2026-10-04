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

// ---------- 小区 / 楼洞 ----------

func listCommunities(c *gin.Context) {
	rows, err := db.Query(`SELECT id, name, fund_type, public_opening, first_rate FROM communities ORDER BY name`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id int64
		var name, fundType string
		var publicOpening int64
		var firstRate float64
		rows.Scan(&id, &name, &fundType, &publicOpening, &firstRate)
		out = append(out, gin.H{"id": id, "name": name, "fundType": fundType,
			"publicOpening": centsToYuan(publicOpening), "firstRate": firstRate})
	}
	c.JSON(http.StatusOK, out)
}

func createCommunity(c *gin.Context) {
	var req struct {
		Name      string  `json:"name" binding:"required"`
		FundType  string  `json:"fundType"`
		FirstRate float64 `json:"firstRate"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "小区名称不能为空"})
		return
	}
	if req.FundType != "public" {
		req.FundType = "commercial"
	}
	if req.FirstRate < 0 {
		req.FirstRate = 0
	}
	res, err := db.Exec(`INSERT INTO communities(name, fund_type, first_rate) VALUES(?,?,?)`, req.Name, req.FundType, req.FirstRate)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "小区「" + req.Name + "」已存在"})
		return
	}
	id, _ := res.LastInsertId()
	c.JSON(http.StatusOK, gin.H{"id": id, "name": req.Name, "fundType": req.FundType, "firstRate": req.FirstRate})
}

// PUT /api/communities/:id  小区设置：名称 / 首期交存标准（元/㎡）/ 公共账期初（元）
// 公共账期初修改时若期初建账凭证已生成，则同步作废旧凭证并按新口径重新生成（须未月结）。
func updateCommunity(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效 id"})
		return
	}
	var req struct {
		Name          *string  `json:"name"`
		FirstRate     *float64 `json:"firstRate"`
		PublicOpening *float64 `json:"publicOpening"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误：" + err.Error()})
		return
	}
	var name, fundType string
	if err := db.QueryRow(`SELECT name, fund_type FROM communities WHERE id=?`, id).Scan(&name, &fundType); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "小区不存在"})
		return
	}

	// 公共账期初：需要联动期初建账凭证时走事务
	if req.PublicOpening != nil {
		if *req.PublicOpening < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "公共账期初不能为负"})
			return
		}
		newOpening := yuanToCents(*req.PublicOpening)
		var oldOpening int64
		db.QueryRow(`SELECT public_opening FROM communities WHERE id=?`, id).Scan(&oldOpening)
		if newOpening == oldOpening {
			req.PublicOpening = nil // 无变化，跳过
		} else {
			tx, err := db.Begin()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			defer tx.Rollback()
			// 已生成期初建账凭证：先校验其月份未月结，再作废并按新口径重新生成
			var gvID int64
			var gvDate string
			err = tx.QueryRow(`SELECT id, date FROM gl_vouchers WHERE source_type='opening' AND source_id=? AND status='normal'`, id).Scan(&gvID, &gvDate)
			if err == nil {
				if msg := lockError(gvDate[:7]); msg != "" {
					c.JSON(http.StatusBadRequest, gin.H{"error": "期初建账凭证所在月份（" + gvDate[:7] + "）" + msg + "，请先解锁再修改公共账期初"})
					return
				}
				if _, err := tx.Exec(`UPDATE gl_vouchers SET status='voided' WHERE id=?`, gvID); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
					return
				}
			}
			if _, err := tx.Exec(`UPDATE communities SET public_opening=? WHERE id=?`, newOpening, id); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			if err == nil {
				// 原期初建账凭证存在 → 按新口径重新生成（期初建账金额 = 户账期初 + 公共账期初）
				var hhOpening int64
				tx.QueryRow(`SELECT IFNULL(SUM(h.opening_balance),0) FROM households h JOIN buildings b ON h.building_id=b.id WHERE b.community_id=?`, id).Scan(&hhOpening)
				total := hhOpening + newOpening
				if total > 0 {
					entries := []glEntry{
						{subject: glSlot(fundType, "bank"), project: id, dir: "debit", amount: total},
					}
					if hhOpening > 0 {
						entries = append(entries, glEntry{subject: glSlot(fundType, "netasset"), project: id, dir: "credit", amount: hhOpening})
					}
					if newOpening > 0 {
						entries = append(entries, glEntry{subject: glSlot(fundType, "pending"), project: id, dir: "credit", amount: newOpening})
					}
					if _, err := glInsertTx(tx, gvDate, gvDate[:7], "opening", "opening", id, "期初建账｜"+name, entries, c.GetString("authUser")); err != nil {
						c.JSON(http.StatusInternalServerError, gin.H{"error": "重新生成期初建账凭证失败：" + err.Error()})
						return
					}
				}
			}
			if err := tx.Commit(); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}
	}
	if req.Name != nil && strings.TrimSpace(*req.Name) != "" && *req.Name != name {
		if _, err := db.Exec(`UPDATE communities SET name=? WHERE id=?`, strings.TrimSpace(*req.Name), id); err != nil {
			c.JSON(http.StatusConflict, gin.H{"error": "小区名称重复"})
			return
		}
	}
	if req.FirstRate != nil {
		if *req.FirstRate < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "首期交存标准不能为负"})
			return
		}
		if _, err := db.Exec(`UPDATE communities SET first_rate=? WHERE id=?`, *req.FirstRate, id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
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

// firstPaymentExpr 首期交存额口径：小区设置首期交存标准（元/㎡）时 = 标准 × 面积；
// 未设置时以建账期初余额近似。续筹红线 = 首期交存额的 30%。
const firstPaymentExpr = `CASE WHEN c.first_rate > 0 THEN CAST(ROUND(c.first_rate * h.area * 100) AS INTEGER) ELSE h.opening_balance END`

// belowThreshold 分户余额是否低于首期交存额 30% 红线（整数分运算避免浮点误差）
func belowThreshold(balance, firstPayment int64) bool {
	return firstPayment > 0 && balance*10 < firstPayment*3
}

func listHouseholds(c *gin.Context) {
	communityID := c.Query("communityId")
	buildingID := c.Query("buildingId")
	keyword := c.Query("keyword")
	sqlStr := `SELECT h.id, b.community_id, c.name AS community, b.name AS building,
		h.room_no, h.owner, h.area, h.opening_balance,
		(SELECT COUNT(*) FROM vouchers v WHERE v.household_id = h.id) AS has_voucher,
		h.opening_balance + ` + replaceAlias(householdDeltaExpr, "h") + ` AS balance,
		` + firstPaymentExpr + ` AS first_payment
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
		var opening, balance, firstPayment int64
		var hasVoucher int
		rows.Scan(&id, &communityIDv, &community, &building, &room, &owner, &area, &opening, &hasVoucher, &balance, &firstPayment)
		out = append(out, gin.H{
			"id": id, "communityId": communityIDv, "community": community,
			"building": building, "roomNo": room, "owner": owner,
			"area": area, "openingBalance": centsToYuan(opening), "hasVoucher": hasVoucher,
			"balance":      centsToYuan(balance),
			"firstPayment": centsToYuan(firstPayment), "belowThreshold": belowThreshold(balance, firstPayment),
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
		if vtype == "income" || vtype == "interest_alloc_child" {
			running += amount
		} else if vtype == "allocate" || vtype == "refund" {
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
		v.amount, v.summary, v.master_id, v.status, v.refund_kind, v.biz_kind,
		v.created_by, v.voided_by, v.void_reason
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
		var status, refundKind, bizKind, createdBy, voidedBy, voidReason string
		if err := rows.Scan(&id, &no, &date, &vtype, &communityID, &community,
			&buildingID, &building, &householdID, &room, &owner,
			&amount, &summary, &masterID, &status, &refundKind, &bizKind,
			&createdBy, &voidedBy, &voidReason); err != nil {
			continue
		}
		out = append(out, gin.H{
			"id": id, "no": no, "date": date, "type": vtype,
			"communityId": communityID, "community": community,
			"buildingId": buildingID.Int64, "building": building.String,
			"householdId": householdID.Int64, "roomNo": room.String, "owner": owner.String,
			"amount": centsToYuan(amount), "summary": summary.String,
			"masterId": masterID.Int64, "status": status, "refundKind": refundKind, "bizKind": bizKind,
			"createdBy": createdBy, "voidedBy": voidedBy, "voidReason": voidReason,
		})
	}
	c.JSON(http.StatusOK, out)
}

type createVoucherReq struct {
	Type         string  `json:"type" binding:"required"`
	Date         string  `json:"date" binding:"required"`
	CommunityID  int64   `json:"communityId" binding:"required"`
	BuildingID   int64   `json:"buildingId"`   // 0 表示未选；expense 且 scope=community 时为全体楼洞
	Scope        string  `json:"scope"`        // expense 用：building | community | selected（指定多户）
	HouseholdID  int64   `json:"householdId"`  // income / refund 用
	HouseholdIDs []int64 `json:"householdIds"` // expense 且 scope=selected 时的目标住户
	Amount       float64 `json:"amount" binding:"required"`
	Summary      string  `json:"summary"`
	Category     string  `json:"category"` // expense 用：engineering|supervision|survey|other；refund 用：destroy|return
	RefundKind   string  `json:"refundKind"`
	PayMethod    string  `json:"payMethod"`    // expense 用：bank（默认）| cash（备用金支付）
	IncomeKind   string  `json:"incomeKind"`   // fund_income 用：business|disposal|other
	CashKind     string  `json:"cashKind"`     // cash 用：withdraw|return
	BondKind     string  `json:"bondKind"`     // bond 用：buy|redeem
	BondInterest float64 `json:"bondInterest"` // bond redeem 时的利息部分
	// ConfirmInsufficient 分摊目标中存在余额不足的户时，须前端二次确认后带 true 提交
	ConfirmInsufficient bool `json:"confirmInsufficient"`
}

// nextVoucherNoTx 业务凭证号 = 当日前缀 + 现有最大号 +1（MAX 口径，删除/作废造成断号时不会撞号；
// 子凭证号带 -A 后缀，按数字位提取比较不受影响）
func nextVoucherNoTx(tx *sql.Tx, date string) (string, error) {
	prefix := "PZ" + strings.ReplaceAll(date, "-", "")
	var n int64
	if err := tx.QueryRow(`SELECT IFNULL(MAX(CAST(substr(no, ?, 4) AS INTEGER)), 0)
		FROM vouchers WHERE no LIKE ?`, len(prefix)+2, prefix+"-%").Scan(&n); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%04d", prefix, n+1), nil
}

// expenseTarget 分摊目标住户
type expenseTarget struct {
	id         int64
	buildingID int64
	area       float64
	roomNo     string
	owner      string
}

// dbQueryer 同时满足 *sql.DB 与 *sql.Tx 的查询接口（预览走 DB，落账走事务）
type dbQueryer interface {
	Query(query string, args ...interface{}) (*sql.Rows, error)
	QueryRow(query string, args ...interface{}) *sql.Row
}

// expenseTargetsTx 按分摊范围取目标住户：building=单楼洞 / community=全小区 / selected=指定多户
func expenseTargetsTx(q dbQueryer, communityID, buildingID int64, scope string, householdIDs []int64) ([]expenseTarget, error) {
	var query string
	var args []interface{}
	switch {
	case scope == "selected" && len(householdIDs) > 0:
		placeholders := strings.TrimRight(strings.Repeat("?,", len(householdIDs)), ",")
		query = `SELECT h.id, h.building_id, h.area, h.room_no, h.owner FROM households h
			JOIN buildings b ON h.building_id=b.id
			WHERE h.id IN (` + placeholders + `) AND b.community_id=? ORDER BY h.id`
		for _, id := range householdIDs {
			args = append(args, id)
		}
		args = append(args, communityID)
	case scope == "community" || buildingID <= 0:
		query = `SELECT h.id, h.building_id, h.area, h.room_no, h.owner FROM households h
			JOIN buildings b ON h.building_id=b.id WHERE b.community_id=? ORDER BY h.id`
		args = []interface{}{communityID}
	default:
		query = `SELECT h.id, h.building_id, h.area, h.room_no, h.owner FROM households h WHERE h.building_id=? ORDER BY h.id`
		args = []interface{}{buildingID}
	}
	rows, err := q.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	targets := []expenseTarget{}
	for rows.Next() {
		var t expenseTarget
		if err := rows.Scan(&t.id, &t.buildingID, &t.area, &t.roomNo, &t.owner); err != nil {
			return nil, err
		}
		targets = append(targets, t)
	}
	return targets, nil
}

// householdBalanceTx 住户当前余额（分）：期初 + 缴存 + 利息分配 - 维修分摊 - 返还/退返
func householdBalanceTx(q dbQueryer, householdID int64) (int64, error) {
	var opening, delta int64
	if err := q.QueryRow(`SELECT opening_balance FROM households WHERE id=?`, householdID).Scan(&opening); err != nil {
		return 0, err
	}
	if err := q.QueryRow(`SELECT IFNULL(SUM(CASE v.type WHEN 'income' THEN v.amount WHEN 'interest_alloc_child' THEN v.amount
		WHEN 'allocate' THEN -v.amount WHEN 'refund' THEN -v.amount ELSE 0 END),0)
		FROM vouchers v WHERE v.household_id=? AND v.status='normal'`, householdID).Scan(&delta); err != nil {
		return 0, err
	}
	return opening + delta, nil
}

// communityPublicBalanceTx 小区公共账可分配余额（分）：公共账期初 + 利息 + 其他收入 - 已分配收益
func communityPublicBalanceTx(q dbQueryer, communityID int64) (int64, error) {
	var opening, delta int64
	if err := q.QueryRow(`SELECT public_opening FROM communities WHERE id=?`, communityID).Scan(&opening); err != nil {
		return 0, err
	}
	if err := q.QueryRow(`SELECT IFNULL(SUM(CASE v.type WHEN 'interest' THEN v.amount WHEN 'fund_income' THEN v.amount WHEN 'interest_alloc' THEN -v.amount ELSE 0 END),0)
		FROM vouchers v WHERE v.community_id=? AND v.status='normal'`, communityID).Scan(&delta); err != nil {
		return 0, err
	}
	return opening + delta, nil
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
	if msg := lockError(req.Date[:7]); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": msg})
		return
	}

	tx, err := db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer tx.Rollback()
	now := time.Now().Format(time.RFC3339)
	user := c.GetString("authUser") // 制单人
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
		// 手工收款防重复：10 分钟内同户同日同金额同摘要视为重复提交（批量导入另有判重规则）
		var dupCnt int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM vouchers WHERE type='income' AND status='normal'
			AND household_id=? AND date=? AND amount=? AND summary=? AND created_at >= ?`,
			req.HouseholdID, req.Date, amountCents, req.Summary,
			time.Now().Add(-10*time.Minute).Format(time.RFC3339)).Scan(&dupCnt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if dupCnt > 0 {
			c.JSON(http.StatusConflict, gin.H{"error": "重复提交：短时间内已存在相同的收款记录（户、日期、金额、摘要均相同），请勿重复保存"})
			return
		}
		if _, err := tx.Exec(`INSERT INTO vouchers(no,date,type,community_id,building_id,household_id,amount,summary,status,created_at,created_by)
			VALUES(?,?,?,?,?,?,?,?, 'normal', ?, ?)`,
			no, req.Date, "income", req.CommunityID, req.BuildingID, req.HouseholdID, amountCents, req.Summary, now, user); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		var bizID int64
		tx.QueryRow(`SELECT last_insert_rowid()`).Scan(&bizID)
		if err := generateBusinessGL(tx, bizGLReq{Type: "income", Date: req.Date, CommunityID: req.CommunityID,
			Amount: amountCents, Summary: req.Summary, BizID: bizID, CreatedBy: user}); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "生成财务凭证失败：" + err.Error()})
			return
		}
		if !commitTxInvalidate(tx, c, req.Date[:7]) {
			return
		}
		c.JSON(http.StatusOK, gin.H{"no": no, "id": bizID})
		return

	case "interest":
		if _, err := tx.Exec(`INSERT INTO vouchers(no,date,type,community_id,building_id,household_id,amount,summary,status,created_at,created_by)
			VALUES(?,?,?,?,NULL,NULL,?,?,'normal',?,?)`,
			no, req.Date, "interest", req.CommunityID, amountCents, req.Summary, now, user); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		var bizID int64
		tx.QueryRow(`SELECT last_insert_rowid()`).Scan(&bizID)
		if err := generateBusinessGL(tx, bizGLReq{Type: "interest", Date: req.Date, CommunityID: req.CommunityID,
			Amount: amountCents, Summary: req.Summary, BizID: bizID, CreatedBy: user}); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "生成财务凭证失败：" + err.Error()})
			return
		}
		if !commitTxInvalidate(tx, c, req.Date[:7]) {
			return
		}
		c.JSON(http.StatusOK, gin.H{"no": no, "id": bizID})
		return

	case "expense":
		if req.Scope == "selected" && len(req.HouseholdIDs) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "指定住户分摊必须提供 householdIds"})
			return
		}
		targets, err := expenseTargetsTx(tx, req.CommunityID, req.BuildingID, req.Scope, req.HouseholdIDs)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if len(targets) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "该范围内没有住户，无法分摊"})
			return
		}
		var masterBuilding interface{}
		if req.Scope == "building" && req.BuildingID > 0 {
			masterBuilding = req.BuildingID
		}
		if _, err := tx.Exec(`INSERT INTO vouchers(no,date,type,community_id,building_id,household_id,amount,summary,expense_category,status,created_at,created_by)
			VALUES(?,?,?,?,?,NULL,?,?,?, 'normal', ?, ?)`,
			no, req.Date, "expense", req.CommunityID, masterBuilding, amountCents, req.Summary, req.Category, now, user); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		var masterID int64
		tx.QueryRow(`SELECT last_insert_rowid()`).Scan(&masterID)
		if err := generateBusinessGL(tx, bizGLReq{Type: "expense", Date: req.Date, CommunityID: req.CommunityID,
			Amount: amountCents, Category: req.Category, PayMethod: req.PayMethod, Summary: req.Summary, BizID: masterID, CreatedBy: user}); err != nil {
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
		// 分摊前校验：目标户余额不足以承担分摊额的，必须先经前端二次确认（confirmInsufficient）
		insufficient := []string{}
		for i, t := range targets {
			if shares[i] == 0 {
				continue
			}
			bal, err := householdBalanceTx(tx, t.id)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			if shares[i] > bal {
				insufficient = append(insufficient, fmt.Sprintf("%s（差 ¥%.2f）", t.roomNo, centsToYuan(shares[i]-bal)))
				if len(insufficient) >= 10 {
					break
				}
			}
		}
		if len(insufficient) > 0 && !req.ConfirmInsufficient {
			c.JSON(http.StatusBadRequest, gin.H{"error": "分摊后有住户余额不足：" + strings.Join(insufficient, "、") + "。请核对后勾选确认再保存"})
			return
		}
		for i, t := range targets {
			if shares[i] == 0 {
				continue
			}
			childNo := fmt.Sprintf("%s-A%03d", no, i+1)
			childSummary := "分摊：" + summary + "（主凭证 " + no + "）"
			if _, err := tx.Exec(`INSERT INTO vouchers(no,date,type,community_id,building_id,household_id,amount,summary,master_id,status,created_at,created_by)
				VALUES(?,?,?,?,?,?,?,?,?,'normal',?,?)`,
				childNo, req.Date, "allocate", req.CommunityID, t.buildingID, t.id,
				shares[i], childSummary, masterID, now, user); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "生成分摊凭证失败：" + err.Error()})
				return
			}
		}
		if !commitTxInvalidate(tx, c, req.Date[:7]) {
			return
		}
		resp := gin.H{"no": no, "masterId": masterID, "allocations": len(targets)}
		if len(insufficient) > 0 {
			resp["insufficient"] = len(insufficient)
		}
		c.JSON(http.StatusOK, resp)
		return

	case "refund":
		// 灭失返还（destroy）/ 退返交存（return），均记到单户、减少该户余额
		if req.BuildingID <= 0 || req.HouseholdID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "返还/退返必须选择楼洞和户室"})
			return
		}
		if req.RefundKind != "destroy" && req.RefundKind != "return" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "退款类型必须为 destroy（灭失返还）或 return（退返）"})
			return
		}
		var hhCnt int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM households h JOIN buildings b ON h.building_id=b.id
			WHERE h.id=? AND h.building_id=? AND b.community_id=?`,
			req.HouseholdID, req.BuildingID, req.CommunityID).Scan(&hhCnt); err != nil || hhCnt == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "所选户室不属于该小区/楼洞，请重新选择"})
			return
		}
		if _, err := tx.Exec(`INSERT INTO vouchers(no,date,type,community_id,building_id,household_id,amount,summary,refund_kind,status,created_at,created_by)
			VALUES(?,?,?,?,?,?,?,?,?,'normal',?,?)`,
			no, req.Date, "refund", req.CommunityID, req.BuildingID, req.HouseholdID, amountCents, req.Summary, req.RefundKind, now, user); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		var bizID int64
		tx.QueryRow(`SELECT last_insert_rowid()`).Scan(&bizID)
		if err := generateBusinessGL(tx, bizGLReq{Type: "refund", Date: req.Date, CommunityID: req.CommunityID,
			Amount: amountCents, Category: req.RefundKind, Summary: req.Summary, BizID: bizID, CreatedBy: user}); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "生成财务凭证失败：" + err.Error()})
			return
		}
		if !commitTxInvalidate(tx, c, req.Date[:7]) {
			return
		}
		c.JSON(http.StatusOK, gin.H{"no": no, "id": bizID})
		return

	case "interest_alloc":
		// 利息分配：从小区公共账（利息-已分配）按建筑面积分配到各户，主凭证 + 分摊子凭证
		avail, err := communityPublicBalanceTx(tx, req.CommunityID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if amountCents > avail {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("分配金额超过可分配公共账余额 ¥%.2f，请先核对利息入账", centsToYuan(avail))})
			return
		}
		targets, err := expenseTargetsTx(tx, req.CommunityID, 0, "community", nil)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if len(targets) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "该小区没有住户，无法分配"})
			return
		}
		if _, err := tx.Exec(`INSERT INTO vouchers(no,date,type,community_id,building_id,household_id,amount,summary,status,created_at)
			VALUES(?,?,?,?,NULL,NULL,?,?,'normal',?)`,
			no, req.Date, "interest_alloc", req.CommunityID, amountCents, req.Summary, now); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		var masterID int64
		tx.QueryRow(`SELECT last_insert_rowid()`).Scan(&masterID)
		// 财务分录：借 待分配累计收益 / 贷 维修资金（净资产），收益分配入户
		var fundType string
		tx.QueryRow(`SELECT fund_type FROM communities WHERE id=?`, req.CommunityID).Scan(&fundType)
		if _, err := glInsertTx(tx, req.Date, req.Date[:7], "business", "voucher", masterID,
			"收益分配｜"+req.Summary, []glEntry{
				{subject: glSlot(fundType, "pending"), project: req.CommunityID, dir: "debit", amount: amountCents},
				{subject: glSlot(fundType, "netasset"), project: req.CommunityID, dir: "credit", amount: amountCents},
			}, user); err != nil {
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
			summary = "利息分配"
		}
		for i, t := range targets {
			if shares[i] == 0 {
				continue
			}
			childNo := fmt.Sprintf("%s-A%03d", no, i+1)
			if _, err := tx.Exec(`INSERT INTO vouchers(no,date,type,community_id,building_id,household_id,amount,summary,master_id,status,created_at,created_by)
				VALUES(?,?,?,?,?,?,?,?,?,'normal',?,?)`,
				childNo, req.Date, "interest_alloc_child", req.CommunityID, t.buildingID, t.id,
				shares[i], "利息分配："+summary+"（主凭证 "+no+"）", masterID, now, user); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "生成利息分配子凭证失败：" + err.Error()})
				return
			}
		}
		if !commitTxInvalidate(tx, c, req.Date[:7]) {
			return
		}
		c.JSON(http.StatusOK, gin.H{"no": no, "masterId": masterID, "allocations": len(targets)})
		return

	case "fund_income":
		// 经营收入 / 共用设施处置收入 / 其他收入（小区级，计入公共账可分配收益）
		if req.IncomeKind != "business" && req.IncomeKind != "disposal" && req.IncomeKind != "other" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "收入类别必须为 business（经营收入）/ disposal（共用设施处置收入）/ other（其他收入）"})
			return
		}
		if _, err := tx.Exec(`INSERT INTO vouchers(no,date,type,community_id,building_id,household_id,amount,summary,biz_kind,status,created_at,created_by)
			VALUES(?,?,?,?,NULL,NULL,?,?,?,'normal',?,?)`,
			no, req.Date, "fund_income", req.CommunityID, amountCents, req.Summary, req.IncomeKind, now, user); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		var bizID int64
		tx.QueryRow(`SELECT last_insert_rowid()`).Scan(&bizID)
		if err := generateBusinessGL(tx, bizGLReq{Type: "fund_income", Date: req.Date, CommunityID: req.CommunityID,
			Amount: amountCents, Category: req.IncomeKind, Summary: req.Summary, BizID: bizID, CreatedBy: user}); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "生成财务凭证失败：" + err.Error()})
			return
		}
		if !commitTxInvalidate(tx, c, req.Date[:7]) {
			return
		}
		c.JSON(http.StatusOK, gin.H{"no": no, "id": bizID})
		return

	case "cash":
		// 备用金：withdraw=提取备用金；return=备用金退回银行（不影响户账/公共账，仅为资金形态转换）
		if req.CashKind != "withdraw" && req.CashKind != "return" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "备用金业务必须为 withdraw（提取）或 return（退回）"})
			return
		}
		if _, err := tx.Exec(`INSERT INTO vouchers(no,date,type,community_id,building_id,household_id,amount,summary,biz_kind,status,created_at,created_by)
			VALUES(?,?,?,?,NULL,NULL,?,?,?,'normal',?,?)`,
			no, req.Date, "cash", req.CommunityID, amountCents, req.Summary, req.CashKind, now, user); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		var bizID int64
		tx.QueryRow(`SELECT last_insert_rowid()`).Scan(&bizID)
		if err := generateBusinessGL(tx, bizGLReq{Type: "cash", Date: req.Date, CommunityID: req.CommunityID,
			Amount: amountCents, Category: req.CashKind, Summary: req.Summary, BizID: bizID, CreatedBy: user}); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "生成财务凭证失败：" + err.Error()})
			return
		}
		if !commitTxInvalidate(tx, c, req.Date[:7]) {
			return
		}
		c.JSON(http.StatusOK, gin.H{"no": no, "id": bizID})
		return

	case "bond":
		// 国债投资：buy=购买（借国债投资/贷国债专户）；redeem=到期兑付（借国债专户/贷国债投资+国债利息收入）
		if req.BondKind != "buy" && req.BondKind != "redeem" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "国债业务必须为 buy（购买）或 redeem（到期兑付）"})
			return
		}
		interestCents := int64(0)
		if req.BondKind == "redeem" {
			if req.BondInterest < 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "国债利息不能为负"})
				return
			}
			interestCents = yuanToCents(req.BondInterest)
		}
		if _, err := tx.Exec(`INSERT INTO vouchers(no,date,type,community_id,building_id,household_id,amount,summary,biz_kind,status,created_at,created_by)
			VALUES(?,?,?,?,NULL,NULL,?,?,?,'normal',?,?)`,
			no, req.Date, "bond", req.CommunityID, amountCents, req.Summary, req.BondKind, now, user); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		var bizID int64
		tx.QueryRow(`SELECT last_insert_rowid()`).Scan(&bizID)
		// 兑付利息同步生成一张挂主凭证的"利息"业务凭证（计入小区公共账可分配收益）；
		// 财务分录由汇总重建时并入兑付凭证，此处不单独生成
		if req.BondKind == "redeem" && interestCents > 0 {
			companionNo := fmt.Sprintf("%s-I001", no)
			if _, err := tx.Exec(`INSERT INTO vouchers(no,date,type,community_id,building_id,household_id,amount,summary,master_id,status,created_at,created_by)
				VALUES(?,?,?,?,NULL,NULL,?,?,?,'normal',?,?)`,
				companionNo, req.Date, "interest", req.CommunityID, interestCents, "国债利息收入："+req.Summary, bizID, now, user); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "生成国债利息业务凭证失败：" + err.Error()})
				return
			}
		}
		if err := generateBusinessGL(tx, bizGLReq{Type: "bond", Date: req.Date, CommunityID: req.CommunityID,
			Amount: amountCents, Extra: interestCents, Category: req.BondKind, Summary: req.Summary, BizID: bizID, CreatedBy: user}); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "生成财务凭证失败：" + err.Error()})
			return
		}
		if !commitTxInvalidate(tx, c, req.Date[:7]) {
			return
		}
		c.JSON(http.StatusOK, gin.H{"no": no, "id": bizID})
		return

	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "未知的凭证类型"})
		return
	}
}

// POST /api/vouchers/expense-preview  维修支出分摊预览（保存前确认金额、户数、余额不足警告）
func previewExpense(c *gin.Context) {
	var req struct {
		CommunityID  int64   `json:"communityId" binding:"required"`
		BuildingID   int64   `json:"buildingId"`
		Scope        string  `json:"scope"`
		HouseholdIDs []int64 `json:"householdIds"`
		Amount       float64 `json:"amount" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Amount <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误：communityId 与正数金额必填"})
		return
	}
	amountCents := yuanToCents(req.Amount)
	if amountCents <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "金额至少 0.01 元"})
		return
	}
	if req.Scope == "selected" && len(req.HouseholdIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "指定住户分摊必须提供 householdIds"})
		return
	}
	targets, err := expenseTargetsTx(db, req.CommunityID, req.BuildingID, req.Scope, req.HouseholdIDs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if len(targets) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该范围内没有住户，无法分摊"})
		return
	}
	areas := make([]float64, len(targets))
	for i, t := range targets {
		areas[i] = t.area
	}
	shares := splitByArea(amountCents, areas)
	rows := []gin.H{}
	var totalArea float64
	insufficientCnt := 0
	for i, t := range targets {
		totalArea += t.area
		bal, err := householdBalanceTx(db, t.id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		deficit := shares[i] - bal
		if deficit > 0 {
			insufficientCnt++
		}
		rows = append(rows, gin.H{
			"householdId": t.id, "roomNo": t.roomNo, "owner": t.owner,
			"area": t.area, "balance": centsToYuan(bal), "share": centsToYuan(shares[i]),
			"deficit": centsToYuan(deficit),
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"targets": rows, "targetCount": len(targets), "totalArea": totalArea,
		"amount": centsToYuan(amountCents), "insufficientCount": insufficientCnt,
	})
}

// POST /api/vouchers/import  批量导入交存流水（银行代收文件）自动生成收款凭证
// rows: [{date, community, building, room, amount, summary}]；小区/楼洞/户号按名称解析，不存在或已月结的行跳过并报错。
func importVouchers(c *gin.Context) {
	var req struct {
		Rows []struct {
			Date      string  `json:"date" binding:"required"`
			Community string  `json:"community" binding:"required"`
			Building  string  `json:"building" binding:"required"`
			Room      string  `json:"room" binding:"required"`
			Amount    float64 `json:"amount" binding:"required"`
			Summary   string  `json:"summary"`
		} `json:"rows" binding:"required"`
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
	now := time.Now().Format(time.RFC3339)
	user := c.GetString("authUser")
	inserted, skipped := 0, 0
	errs := []string{}
	nos := []string{}
	touchedMonths := map[string]bool{}
	touchedDays := map[string]bool{}
	for i, r := range req.Rows {
		lineNo := i + 2
		if _, err := time.Parse("2006-01-02", r.Date); err != nil {
			errs = append(errs, strconv.Itoa(lineNo)+" 行：日期格式错误（应为 yyyy-MM-dd）")
			skipped++
			continue
		}
		amountCents := yuanToCents(r.Amount)
		if amountCents <= 0 {
			errs = append(errs, strconv.Itoa(lineNo)+" 行：金额必须大于 0")
			skipped++
			continue
		}
		if msg := lockError(r.Date[:7]); msg != "" {
			errs = append(errs, strconv.Itoa(lineNo)+" 行："+msg)
			skipped++
			continue
		}
		var cid int64
		if err := tx.QueryRow(`SELECT id FROM communities WHERE name=?`, strings.TrimSpace(r.Community)).Scan(&cid); err != nil {
			errs = append(errs, strconv.Itoa(lineNo)+" 行：小区「"+r.Community+"」不存在")
			skipped++
			continue
		}
		var bid int64
		if err := tx.QueryRow(`SELECT id FROM buildings WHERE community_id=? AND name=?`, cid, strings.TrimSpace(r.Building)).Scan(&bid); err != nil {
			errs = append(errs, strconv.Itoa(lineNo)+" 行：楼洞「"+r.Building+"」不存在")
			skipped++
			continue
		}
		var hid int64
		if err := tx.QueryRow(`SELECT id FROM households WHERE building_id=? AND room_no=?`, bid, strings.TrimSpace(r.Room)).Scan(&hid); err != nil {
			errs = append(errs, strconv.Itoa(lineNo)+" 行：户号「"+r.Room+"」不存在")
			skipped++
			continue
		}
		// 疑似重复：同日同户同金额同摘要的收入凭证已存在（银行批量文件重复导入防护）
		var dup int
		tx.QueryRow(`SELECT COUNT(*) FROM vouchers WHERE type='income' AND status='normal'
			AND date=? AND household_id=? AND amount=? AND summary=?`,
			r.Date, hid, amountCents, r.Summary).Scan(&dup)
		if dup > 0 {
			errs = append(errs, strconv.Itoa(lineNo)+" 行：疑似重复（同日同户同金额已入账），已跳过")
			skipped++
			continue
		}
		no, err := nextVoucherNoTx(tx, r.Date)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if _, err := tx.Exec(`INSERT INTO vouchers(no,date,type,community_id,building_id,household_id,amount,summary,status,created_at,created_by)
			VALUES(?,?,?,?,?,?,?,?,'normal',?,?)`,
			no, r.Date, "income", cid, bid, hid, amountCents, r.Summary, now, user); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		inserted++
		nos = append(nos, no)
		touchedMonths[r.Date[:7]] = true
		touchedDays[fmt.Sprintf("%d|%s", cid, r.Date)] = true
	}
	for key := range touchedDays {
		parts := strings.SplitN(key, "|", 2)
		dcid, _ := strconv.ParseInt(parts[0], 10, 64)
		if err := rebuildCommunityDayGL(tx, dcid, parts[1], user, false); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "重建汇总记账凭证失败：" + err.Error()})
			return
		}
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
	if len(errs) > 20 {
		errs = errs[:20]
	}
	c.JSON(http.StatusOK, gin.H{"inserted": inserted, "skipped": skipped, "errors": errs,
		"firstNo": func() string {
			if len(nos) > 0 {
				return nos[0]
			}
			return ""
		}(),
		"lastNo": func() string {
			if len(nos) > 0 {
				return nos[len(nos)-1]
			}
			return ""
		}()})
}

// commitTxInvalidate 提交前使该月结转失效（改账后需重新结转），并提交事务
func commitTxInvalidate(tx *sql.Tx, c *gin.Context, month string) bool {
	if err := invalidateClosingTx(tx, month); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return false
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return false
	}
	return true
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
	var masterID sql.NullInt64
	db.QueryRow(`SELECT master_id FROM vouchers WHERE id=?`, id).Scan(&masterID)
	if vtype == "allocate" || vtype == "interest_alloc_child" || (masterID.Valid && masterID.Int64 > 0) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该凭证随主凭证管理，请通过作废主凭证处理"})
		return
	}
	if msg := lockError(vdate[:7]); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": msg + "，作废前请先解锁"})
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&req) // 作废原因选填，绑定失败不阻断
	tx, err := db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer tx.Rollback()
	user := c.GetString("authUser")
	reason := strings.TrimSpace(req.Reason)
	if _, err := tx.Exec(`UPDATE vouchers SET status='voided', void_of=?, voided_by=?, void_reason=? WHERE id=?`,
		id, user, reason, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	children := 0
	if vtype == "expense" || vtype == "interest_alloc" || vtype == "bond" {
		res, err := tx.Exec(`UPDATE vouchers SET status='voided', void_of=?, voided_by=?, void_reason=? WHERE master_id=? AND status='normal'`,
			id, user, reason, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if n, e := res.RowsAffected(); e == nil {
			children = int(n)
		}
	}
	// 级联作废对应的汇总记账凭证：按（小区,日期）重建当日汇总
	var vcid int64
	tx.QueryRow(`SELECT community_id FROM vouchers WHERE id=?`, id).Scan(&vcid)
	if err := rebuildCommunityDayGL(tx, vcid, vdate, user, false); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	voidedGL := 0
	if err := invalidateClosingTx(tx, vdate[:7]); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "voidedChildren": children, "voidedGL": voidedGL})
}
