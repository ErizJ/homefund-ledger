package app

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// postJSON 用测试上下文直接调用 POST 处理器并解析 JSON 响应。
func postJSON(t *testing.T, handler gin.HandlerFunc, target string, body interface{}) gin.H {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, target, bytes.NewReader(b))
	c.Request.Header.Set("Content-Type", "application/json")
	handler(c)
	var m gin.H
	json.Unmarshal(w.Body.Bytes(), &m)
	return m
}

// ==================== 结转净额（含冲正反方向） ====================

func TestClosingEntriesNet(t *testing.T) {
	pid := sql.NullInt64{Int64: 1, Valid: true}
	cases := []struct {
		name string
		aggs []plNet
		// 期望分录（科目+方向+金额）
		want []glEntry
	}{
		{
			name: "交存净贷方→净资产",
			aggs: []plNet{{subject: "400101", project: pid, debit: 0, credit: 10000, fundType: "commercial"}},
			want: []glEntry{
				{subject: "400101", project: pid, dir: "debit", amount: 10000},
				{subject: "3001", project: pid, dir: "credit", amount: 10000},
			},
		},
		{
			name: "利息净贷方→待分配收益",
			aggs: []plNet{{subject: "410101", project: pid, debit: 0, credit: 500, fundType: "commercial"}},
			want: []glEntry{
				{subject: "410101", project: pid, dir: "debit", amount: 500},
				{subject: "310101", project: pid, dir: "credit", amount: 500},
			},
		},
		{
			name: "退返导致交存净借方→反向结转",
			aggs: []plNet{{subject: "400101", project: pid, debit: 3000, credit: 1000, fundType: "commercial"}},
			want: []glEntry{
				{subject: "3001", project: pid, dir: "debit", amount: 2000},
				{subject: "400101", project: pid, dir: "credit", amount: 2000},
			},
		},
		{
			name: "支出净借方→净资产",
			aggs: []plNet{{subject: "500101", project: pid, debit: 8000, credit: 0, fundType: "commercial"}},
			want: []glEntry{
				{subject: "3001", project: pid, dir: "debit", amount: 8000},
				{subject: "500101", project: pid, dir: "credit", amount: 8000},
			},
		},
		{
			name: "支出冲正净贷方→反向结转",
			aggs: []plNet{{subject: "500101", project: pid, debit: 1000, credit: 4000, fundType: "commercial"}},
			want: []glEntry{
				{subject: "500101", project: pid, dir: "debit", amount: 3000},
				{subject: "3001", project: pid, dir: "credit", amount: 3000},
			},
		},
		{
			name: "净额为零不产生分录",
			aggs: []plNet{{subject: "400101", project: pid, debit: 500, credit: 500, fundType: "commercial"}},
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := closingEntriesFromAggs(tc.aggs)
			if len(got) != len(tc.want) {
				t.Fatalf("closingEntriesFromAggs = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("closingEntriesFromAggs = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// ==================== 灭失返还 / 退返 ====================

// seedRefund 插入返还/退返业务凭证并生成财务分录
func seedRefund(t *testing.T, tx *sql.Tx, date, kind string, communityID, householdID, amountCents int64) int64 {
	t.Helper()
	res, err := tx.Exec(`INSERT INTO vouchers(no,date,type,community_id,household_id,amount,summary,refund_kind,status,created_at)
		VALUES(?,?,?,?,?,?,?,?,'normal',?)`,
		nextTestNo(), date, "refund", communityID, householdID, amountCents, "返还", kind, time.Now().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("seedRefund insert: %v", err)
	}
	id, _ := res.LastInsertId()
	if err := generateBusinessGL(tx, bizGLReq{Type: "refund", Date: date, CommunityID: communityID,
		Amount: amountCents, Category: kind, Summary: "返还", BizID: id, CreatedBy: "test"}); err != nil {
		t.Fatalf("generateBusinessGL(refund): %v", err)
	}
	return id
}

func TestRefundFlow(t *testing.T) {
	newTestDB(t)
	cid := seedCommunity(t, "返还小区", "commercial")
	bid := seedBuilding(t, cid, "1栋")
	h1 := seedHousehold(t, bid, "101", 100, 1000000) // 期初 10000 元

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	seedOpeningGL(t, tx, "2026-01-01", cid, "commercial", 1000000)
	seedBizVoucher(t, tx, "2026-09-05", "income", cid, h1, 5000000, "", "缴存")
	seedRefund(t, tx, "2026-09-10", "return", cid, h1, 2000000)  // 退返 20000 元
	seedRefund(t, tx, "2026-09-15", "destroy", cid, h1, 1000000) // 灭失返还 10000 元
	if err := closeMonthTx(tx, "2026-09", "test"); err != nil {
		t.Fatalf("closeMonthTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	// 业务台账：户余额 = 期初 10000 + 缴存 50000 - 退返 20000 - 灭失返还 10000 = 30000 元
	var balance int64
	db.QueryRow(`SELECT h.opening_balance + `+replaceAlias(householdDeltaExpr, "h")+` FROM households h WHERE h.id=?`, h1).Scan(&balance)
	if balance != 3000000 {
		t.Fatalf("户余额 = %d 分，want 3000000", balance)
	}
	// 财务账：净资产 = 期初 10000 + 缴存净额(50000-20000) - 灭失返还 10000 = 30000 元
	if got := subjectBalance(t, "3001", cid); got != 3000000 {
		t.Fatalf("3001 余额 = %d 分，want 3000000", got)
	}
	// 银行 = 期初 10000 + 缴存 50000 - 退返 20000 - 返还 10000 = 30000 元
	if got := -subjectBalance(t, "100101", cid); got != 3000000 {
		t.Fatalf("100101 余额 = %d 分，want 3000000", got)
	}
	// 结转后 4/5 类科目本月净额清零（退返借方与缴存贷方净额结转）
	if n := subjectMonthNet(t, "2026-09", "4%"); n != 0 {
		t.Fatalf("结转后 4 类科目本月净额应为 0，got %d", n)
	}
	if n := subjectMonthNet(t, "2026-09", "5%"); n != 0 {
		t.Fatalf("结转后 5 类科目本月净额应为 0，got %d", n)
	}
}

// ==================== 利息分配（公共账 → 各户，按面积） ====================

// seedInterestAlloc 插入利息分配主凭证+子凭证并生成财务分录（借待分配/贷净资产）
func seedInterestAlloc(t *testing.T, tx *sql.Tx, date string, cid, masterAmountCents int64, shares map[int64]int64) int64 {
	t.Helper()
	res, err := tx.Exec(`INSERT INTO vouchers(no,date,type,community_id,amount,summary,status,created_at)
		VALUES(?,?,?,?,?,?,'normal',?)`,
		nextTestNo(), date, "interest_alloc", cid, masterAmountCents, "利息分配", time.Now().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("seedInterestAlloc master: %v", err)
	}
	masterID, _ := res.LastInsertId()
	var fundType string
	tx.QueryRow(`SELECT fund_type FROM communities WHERE id=?`, cid).Scan(&fundType)
	if _, err := glInsertTx(tx, date, date[:7], "business", "voucher", masterID,
		"利息分配", []glEntry{
			{subject: glSlot(fundType, "pending"), project: cid, dir: "debit", amount: masterAmountCents},
			{subject: glSlot(fundType, "netasset"), project: cid, dir: "credit", amount: masterAmountCents},
		}, "test"); err != nil {
		t.Fatalf("seedInterestAlloc GL: %v", err)
	}
	i := 0
	for hid, amt := range shares {
		i++
		if _, err := tx.Exec(`INSERT INTO vouchers(no,date,type,community_id,household_id,amount,summary,master_id,status,created_at)
			VALUES(?,?,?,?,?,?,?,?,'normal',?)`,
			fmt.Sprintf("TEST-ALLOC-%d", i), date, "interest_alloc_child", cid, hid, amt, "利息分配", masterID, time.Now().Format(time.RFC3339)); err != nil {
			t.Fatalf("seedInterestAlloc child: %v", err)
		}
	}
	return masterID
}

func TestInterestAllocFlow(t *testing.T) {
	newTestDB(t)
	cid := seedCommunity(t, "利息分配小区", "commercial")
	bid := seedBuilding(t, cid, "1栋")
	h1 := seedHousehold(t, bid, "101", 100, 1000000)
	h2 := seedHousehold(t, bid, "102", 100, 1000000)

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	seedOpeningGL(t, tx, "2026-01-01", cid, "commercial", 2000000)
	seedBizVoucher(t, tx, "2026-09-20", "interest", cid, 0, 100000, "", "存款利息") // 利息 1000 元
	seedInterestAlloc(t, tx, "2026-09-25", cid, 100000, map[int64]int64{h1: 50000, h2: 50000})
	if err := closeMonthTx(tx, "2026-09", "test"); err != nil {
		t.Fatalf("closeMonthTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	// 户余额 = 期初 10000 + 利息分配 500 = 10500 元
	for _, hid := range []int64{h1, h2} {
		var balance int64
		db.QueryRow(`SELECT h.opening_balance + `+replaceAlias(householdDeltaExpr, "h")+` FROM households h WHERE h.id=?`, hid).Scan(&balance)
		if balance != 1050000 {
			t.Fatalf("户 %d 余额 = %d 分，want 1050000", hid, balance)
		}
	}
	// 公共账 = 利息 1000 - 已分配 1000 = 0
	avail, err := communityPublicBalanceTx(db, cid)
	if err != nil {
		t.Fatal(err)
	}
	if avail != 0 {
		t.Fatalf("公共账可分配余额 = %d 分，want 0", avail)
	}
	// 待分配收益结转后 = 利息收入 1000 - 分配 1000 = 0
	if got := subjectBalance(t, "310101", cid); got != 0 {
		t.Fatalf("310101 余额 = %d 分，want 0", got)
	}
	// 净资产 = 期初 20000 + 利息分配 1000 = 21000 元
	if got := subjectBalance(t, "3001", cid); got != 2100000 {
		t.Fatalf("3001 余额 = %d 分，want 2100000", got)
	}
	// 双线对账一致
	resp := getJSON(t, glReconcile, "/api/gl/reconcile?month=2026-09")
	row := reconcileRow(t, resp, cid)
	if got := row["netDiff"].(float64); got != 0 {
		t.Fatalf("netDiff = %v，want 0", got)
	}
	if got := row["pendingDiff"].(float64); got != 0 {
		t.Fatalf("pendingDiff = %v，want 0", got)
	}
}

// ==================== 分摊预览与余额不足提示 ====================

func TestPreviewExpenseInsufficient(t *testing.T) {
	newTestDB(t)
	cid := seedCommunity(t, "预览小区", "commercial")
	bid := seedBuilding(t, cid, "1栋")
	seedHousehold(t, bid, "101", 100, 100000)  // 期初 1000 元
	seedHousehold(t, bid, "102", 100, 5000000) // 期初 50000 元

	// 分摊 3000 元：101 户分摊 1500 元 > 余额 1000 元，不足 500 元
	resp := postJSON(t, previewExpense, "/api/vouchers/expense-preview", gin.H{
		"communityId": cid, "buildingId": bid, "scope": "building", "amount": 3000,
	})
	if got, _ := resp["insufficientCount"].(float64); got != 1 {
		t.Fatalf("insufficientCount = %v，want 1（resp=%v）", got, resp)
	}
	targets, _ := resp["targets"].([]interface{})
	if len(targets) != 2 {
		t.Fatalf("targets 应为 2 户，got %d", len(targets))
	}
	var deficit101 float64
	for _, tr := range targets {
		m := tr.(map[string]interface{})
		if m["roomNo"] == "101" {
			deficit101 = m["deficit"].(float64)
		}
	}
	if deficit101 != 500 {
		t.Fatalf("101 户 deficit = %v，want 500", deficit101)
	}
}

func TestPreviewExpenseSelectedScope(t *testing.T) {
	newTestDB(t)
	cid := seedCommunity(t, "多选小区", "commercial")
	bid := seedBuilding(t, cid, "1栋")
	h1 := seedHousehold(t, bid, "101", 100, 0)
	h2 := seedHousehold(t, bid, "102", 100, 0)
	h3 := seedHousehold(t, bid, "103", 100, 0)

	// 只分摊 101/103 两户
	resp := postJSON(t, previewExpense, "/api/vouchers/expense-preview", gin.H{
		"communityId": cid, "scope": "selected", "householdIds": []int64{h1, h3}, "amount": 100,
	})
	if got, _ := resp["targetCount"].(float64); got != 2 {
		t.Fatalf("targetCount = %v，want 2", got)
	}
	targets, _ := resp["targets"].([]interface{})
	for _, tr := range targets {
		m := tr.(map[string]interface{})
		if m["share"].(float64) != 50 {
			t.Fatalf("各户分摊应为 50 元，got %v", m["share"])
		}
	}
	_ = h2
}

// ==================== 批量导入交存流水 ====================

func TestImportVouchers(t *testing.T) {
	newTestDB(t)
	cid := seedCommunity(t, "导入小区", "commercial")
	bid := seedBuilding(t, cid, "1栋")
	h1 := seedHousehold(t, bid, "101", 100, 0)
	seedHousehold(t, bid, "102", 100, 0)

	resp := postJSON(t, importVouchers, "/api/vouchers/import", gin.H{
		"rows": []gin.H{
			{"date": "2026-09-01", "community": "导入小区", "building": "1栋", "room": "101", "amount": 500, "summary": "批量交存"},
			{"date": "2026-09-02", "community": "导入小区", "building": "1栋", "room": "999", "amount": 100, "summary": "坏行"},
			{"date": "2026-09-03", "community": "不存在小区", "building": "1栋", "room": "101", "amount": 100, "summary": "坏行"},
		},
	})
	if got, _ := resp["inserted"].(float64); got != 1 {
		t.Fatalf("inserted = %v，want 1（resp=%v）", got, resp)
	}
	if got, _ := resp["skipped"].(float64); got != 2 {
		t.Fatalf("skipped = %v，want 2（resp=%v）", got, resp)
	}
	// 重复导入同一行 → 视为重复跳过
	resp2 := postJSON(t, importVouchers, "/api/vouchers/import", gin.H{
		"rows": []gin.H{
			{"date": "2026-09-01", "community": "导入小区", "building": "1栋", "room": "101", "amount": 500, "summary": "批量交存"},
		},
	})
	if got, _ := resp2["skipped"].(float64); got != 1 {
		t.Fatalf("重复行应跳过，skipped = %v（resp=%v）", got, resp2)
	}
	// 业务凭证 + 财务凭证均生成
	var bizCnt, glCnt int
	db.QueryRow(`SELECT COUNT(*) FROM vouchers WHERE type='income' AND status='normal'`).Scan(&bizCnt)
	db.QueryRow(`SELECT COUNT(*) FROM gl_vouchers WHERE source_type='agg' AND status='normal'`).Scan(&glCnt)
	if bizCnt != 1 || glCnt != 1 {
		t.Fatalf("bizCnt=%d glCnt=%d，want 1/1", bizCnt, glCnt)
	}
	// 户余额 = 500 元
	var balance int64
	db.QueryRow(`SELECT h.opening_balance + `+replaceAlias(householdDeltaExpr, "h")+` FROM households h WHERE h.id=?`, h1).Scan(&balance)
	if balance != 50000 {
		t.Fatalf("户余额 = %d 分，want 50000", balance)
	}
}

// ==================== 期初建账含公共账期初 ====================

func TestOpeningBalanceWithPublicOpening(t *testing.T) {
	newTestDB(t)
	cid := seedCommunity(t, "公共期初小区", "commercial")
	bid := seedBuilding(t, cid, "1栋")
	seedHousehold(t, bid, "101", 100, 5000000) // 户账期初 50000 元

	// 设置公共账期初 2000 元
	if _, err := db.Exec(`UPDATE communities SET public_opening=200000 WHERE id=?`, cid); err != nil {
		t.Fatal(err)
	}
	resp := postJSON(t, glOpeningBalance, "/api/gl/opening-balance", gin.H{"communityId": cid, "date": "2026-01-01"})
	if _, ok := resp["glVoucherId"]; !ok {
		t.Fatalf("期初建账失败：%v", resp)
	}
	if got, _ := resp["amount"].(float64); got != 52000 {
		t.Fatalf("期初建账金额 = %v，want 52000", got)
	}
	// 净资产 = 户账期初 50000；待分配收益 = 公共账期初 2000；银行 = 52000
	if got := subjectBalance(t, "3001", cid); got != 5000000 {
		t.Fatalf("3001 = %d 分，want 5000000", got)
	}
	if got := subjectBalance(t, "310101", cid); got != 200000 {
		t.Fatalf("310101 = %d 分，want 200000", got)
	}
	if got := -subjectBalance(t, "100101", cid); got != 5200000 {
		t.Fatalf("100101 = %d 分，want 5200000", got)
	}
	// 对账：公共账 = 期初 2000，与待分配收益一致
	resp2 := getJSON(t, glReconcile, "/api/gl/reconcile?month=2026-01")
	row := reconcileRow(t, resp2, cid)
	if got := row["pendingDiff"].(float64); got != 0 {
		t.Fatalf("pendingDiff = %v，want 0", got)
	}
}

// ==================== 30% 续筹红线 ====================

func TestBelowThreshold(t *testing.T) {
	cases := []struct {
		balance, first int64
		want           bool
	}{
		{300, 1000, false}, // 恰好 30%，不触发
		{299, 1000, true},  // 低于 30%
		{0, 1000, true},
		{500, 0, false}, // 无首期交存额不触发
	}
	for _, tc := range cases {
		if got := belowThreshold(tc.balance, tc.first); got != tc.want {
			t.Fatalf("belowThreshold(%d, %d) = %v, want %v", tc.balance, tc.first, got, tc.want)
		}
	}
}

func TestHouseholdListWarning(t *testing.T) {
	newTestDB(t)
	cid := seedCommunity(t, "红线小区", "commercial")
	bid := seedBuilding(t, cid, "1栋")
	// 首期交存标准 100 元/㎡，101 户面积 100 ㎡ → 首期 10000 元，30% = 3000 元
	if _, err := db.Exec(`UPDATE communities SET first_rate=100 WHERE id=?`, cid); err != nil {
		t.Fatal(err)
	}
	seedHousehold(t, bid, "101", 100, 250000) // 余额 2500 元 < 3000 → 预警
	seedHousehold(t, bid, "102", 100, 500000) // 余额 5000 元 → 不预警

	rows, err := db.Query(`SELECT h.opening_balance + `+replaceAlias(householdDeltaExpr, "h")+`,
		`+firstPaymentExpr+`, h.room_no
		FROM households h JOIN buildings b ON h.building_id=b.id JOIN communities c ON b.community_id=c.id
		WHERE b.community_id=?`, cid)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	warns := map[string]bool{}
	for rows.Next() {
		var bal, first int64
		var room string
		rows.Scan(&bal, &first, &room)
		warns[room] = belowThreshold(bal, first)
	}
	if !warns["101"] || warns["102"] {
		t.Fatalf("预警结果 = %v，want 101 预警、102 不预警", warns)
	}
}

// ==================== 旧库 vouchers 表类型扩展迁移 ====================

func TestMigrateVoucherTypes(t *testing.T) {
	dir := t.TempDir()
	path := fmt.Sprintf("%s/old.db", dir)
	d, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// 旧版 vouchers：INTEGER 金额 + 旧 CHECK（无 refund/interest_alloc）
	oldSchema := `
	CREATE TABLE communities (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE, fund_type TEXT NOT NULL DEFAULT 'commercial', public_opening INTEGER NOT NULL DEFAULT 0, first_rate REAL NOT NULL DEFAULT 0);
	CREATE TABLE buildings (id INTEGER PRIMARY KEY AUTOINCREMENT, community_id INTEGER NOT NULL, name TEXT NOT NULL, UNIQUE(community_id, name));
	CREATE TABLE households (
	  id INTEGER PRIMARY KEY AUTOINCREMENT, building_id INTEGER NOT NULL, room_no TEXT NOT NULL,
	  owner TEXT DEFAULT '', area REAL NOT NULL DEFAULT 0, opening_balance INTEGER NOT NULL DEFAULT 0,
	  UNIQUE(building_id, room_no));
	CREATE TABLE vouchers (
	  id INTEGER PRIMARY KEY AUTOINCREMENT, no TEXT NOT NULL UNIQUE, date TEXT NOT NULL,
	  type TEXT NOT NULL CHECK(type IN ('income','expense','interest','allocate')),
	  community_id INTEGER NOT NULL, building_id INTEGER, household_id INTEGER,
	  amount INTEGER NOT NULL, summary TEXT DEFAULT '', master_id INTEGER,
	  status TEXT NOT NULL DEFAULT 'normal', void_of INTEGER, created_at TEXT NOT NULL,
	  expense_category TEXT NOT NULL DEFAULT '');
	CREATE TABLE bank_txns (
	  id INTEGER PRIMARY KEY AUTOINCREMENT, date TEXT NOT NULL, amount INTEGER NOT NULL,
	  summary TEXT DEFAULT '', status TEXT NOT NULL DEFAULT 'unmatched',
	  matched_voucher_id INTEGER, created_at TEXT NOT NULL);
	CREATE TABLE gl_vouchers (
	  id INTEGER PRIMARY KEY AUTOINCREMENT, no TEXT NOT NULL UNIQUE, date TEXT NOT NULL,
	  kind TEXT NOT NULL, source_type TEXT NOT NULL DEFAULT '', source_id INTEGER NOT NULL DEFAULT 0,
	  summary TEXT DEFAULT '', month TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'normal', created_at TEXT NOT NULL);
	CREATE TABLE gl_entries (
	  id INTEGER PRIMARY KEY AUTOINCREMENT, voucher_id INTEGER NOT NULL, subject_code TEXT NOT NULL,
	  project_id INTEGER, direction TEXT NOT NULL, amount INTEGER NOT NULL);`
	if _, err := d.Exec(oldSchema); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Format(time.RFC3339)
	d.Exec(`INSERT INTO communities(id,name) VALUES(1,'旧小区')`)
	d.Exec(`INSERT INTO buildings(id,community_id,name) VALUES(1,1,'1栋')`)
	d.Exec(`INSERT INTO households(id,building_id,room_no,area,opening_balance) VALUES(1,1,'101',88.88,12345)`)
	d.Exec(`INSERT INTO vouchers(id,no,date,type,community_id,household_id,amount,created_at) VALUES(1,'PZ1','2026-09-01','income',1,1,12345,?)`, now)
	d.Close()

	if err := openDB(path); err != nil {
		t.Fatalf("openDB（类型迁移）: %v", err)
	}
	// 存量数据保留
	var amt int64
	if err := db.QueryRow(`SELECT amount FROM vouchers WHERE id=1`).Scan(&amt); err != nil || amt != 12345 {
		t.Fatalf("vouchers#1 amount = %d (%v), want 12345", amt, err)
	}
	// 新类型可插入（CHECK 已扩展）
	if _, err := db.Exec(`INSERT INTO vouchers(no,date,type,community_id,household_id,amount,refund_kind,status,created_at)
		VALUES('PZ-REFUND','2026-09-02','refund',1,1,100,'destroy','normal',?)`, now); err != nil {
		t.Fatalf("插入 refund 凭证失败：%v", err)
	}
	db.Close()
}
