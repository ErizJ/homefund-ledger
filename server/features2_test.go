package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// ==================== 其他收入（经营/处置/其他） ====================

func TestFundIncomeFlow(t *testing.T) {
	newTestDB(t)
	cid := seedCommunity(t, "经营收入小区", "commercial")
	bid := seedBuilding(t, cid, "1栋")
	seedHousehold(t, bid, "101", 100, 1000000)

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	seedOpeningGL(t, tx, "2026-01-01", cid, "commercial", 1000000)
	seedBizVoucher(t, tx, "2026-09-20", "interest", cid, 0, 50000, "", "利息")
	// 经营收入 800 元（记公共账）：先落业务行，再重建当日汇总凭证
	tx.Exec(`INSERT INTO vouchers(no,date,type,community_id,amount,summary,biz_kind,status,created_at,created_by)
		VALUES(?,?,?,?,?,?,?,'normal',?,?)`,
		nextTestNo(), "2026-09-21", "fund_income", cid, 80000, "场地经营", "business", time.Now().Format(time.RFC3339), "test")
	if err := rebuildCommunityDayGL(tx, cid, "2026-09-21", "test", false); err != nil {
		t.Fatalf("rebuildCommunityDayGL: %v", err)
	}
	if err := closeMonthTx(tx, "2026-09", "test"); err != nil {
		t.Fatalf("closeMonthTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	// 公共账 = 利息 500 + 经营收入 800 = 1300
	avail, err := communityPublicBalanceTx(db, cid)
	if err != nil {
		t.Fatal(err)
	}
	if avail != 130000 {
		t.Fatalf("公共账可分配余额 = %d 分，want 130000", avail)
	}
	// 双线对账归零
	resp := getJSON(t, glReconcile, "/api/gl/reconcile?month=2026-09")
	row := reconcileRow(t, resp, cid)
	if row["pendingDiff"].(float64) != 0 {
		t.Fatalf("pendingDiff = %v，want 0", row["pendingDiff"])
	}
	// 收支表：经营收入本月 800
	is := getJSON(t, glIncomeStatement, "/api/gl/income-statement?month=2026-09")
	found := false
	for _, r := range is["income"].([]interface{}) {
		m := r.(map[string]interface{})
		if m["name"] == "经营收入" && m["curTotal"].(float64) == 800 {
			found = true
		}
	}
	if !found {
		t.Fatalf("收支表经营收入应为 800：%v", is["income"])
	}
}

// ==================== 备用金（提取/退回 + 备用金支付维修） ====================

func TestCashFlow(t *testing.T) {
	newTestDB(t)
	cid := seedCommunity(t, "备用金小区", "commercial")
	bid := seedBuilding(t, cid, "1栋")
	seedHousehold(t, bid, "101", 100, 1000000)

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	seedOpeningGL(t, tx, "2026-01-01", cid, "commercial", 1000000)
	// 落三笔业务行后逐日重建汇总凭证
	tx.Exec(`INSERT INTO vouchers(no,date,type,community_id,amount,summary,biz_kind,status,created_at,created_by)
		VALUES(?,?,?,?,?,?,?,'normal',?,?)`, nextTestNo(), "2026-09-01", "cash", cid, 100000, "提取备用金", "withdraw", time.Now().Format(time.RFC3339), "test")
	tx.Exec(`INSERT INTO vouchers(no,date,type,community_id,amount,summary,expense_category,status,created_at,created_by)
		VALUES(?,?,?,?,?,?,?,'normal',?,?)`, nextTestNo(), "2026-09-02", "expense", cid, 30000, "零星维修", "cash", time.Now().Format(time.RFC3339), "test")
	tx.Exec(`INSERT INTO vouchers(no,date,type,community_id,amount,summary,biz_kind,status,created_at,created_by)
		VALUES(?,?,?,?,?,?,?,'normal',?,?)`, nextTestNo(), "2026-09-03", "cash", cid, 20000, "备用金退回", "return", time.Now().Format(time.RFC3339), "test")
	for _, d := range []string{"2026-09-01", "2026-09-02", "2026-09-03"} {
		if err := rebuildCommunityDayGL(tx, cid, d, "test", false); err != nil {
			t.Fatalf("rebuildCommunityDayGL(%s): %v", d, err)
		}
	}
	if err := closeMonthTx(tx, "2026-09", "test"); err != nil {
		t.Fatalf("closeMonthTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	// 备用金余额 = 1000 - 300 - 200 = 500 元
	if got := -subjectBalance(t, "1201", cid); got != 50000 {
		t.Fatalf("1201 余额 = %d 分，want 50000", got)
	}
	// 银行余额 = 期初10000 - 提取1000 + 退回200 = 9200
	if got := -subjectBalance(t, "100101", cid); got != 920000 {
		t.Fatalf("100101 余额 = %d 分，want 920000", got)
	}
	// 维修支出结转：净资产 = 期初10000 - 支出300 = 9700
	if got := subjectBalance(t, "3001", cid); got != 970000 {
		t.Fatalf("3001 余额 = %d 分，want 970000", got)
	}
	// 资产负债表：备用金行 = 500
	bs := getJSON(t, glBalanceSheet, "/api/gl/balance-sheet?month=2026-09")
	found := false
	for _, r := range bs["assets"].([]interface{}) {
		m := r.(map[string]interface{})
		if m["name"] == "备用金" && m["closTotal"].(float64) == 500 {
			found = true
		}
	}
	if !found {
		t.Fatalf("资产负债表备用金行应为 500：%v", bs["assets"])
	}
}

// ==================== 国债投资（购买/到期兑付） ====================

func TestBondFlow(t *testing.T) {
	newTestDB(t)
	cid := seedCommunity(t, "国债小区", "commercial")
	bid := seedBuilding(t, cid, "1栋")
	seedHousehold(t, bid, "101", 100, 1000000)

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	seedOpeningGL(t, tx, "2026-01-01", cid, "commercial", 1000000)
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	// 购买国债 3000 元：借 110101 / 贷 100103
	resp := postJSON(t, createVoucher, "/api/vouchers", gin.H{
		"type": "bond", "date": "2026-09-01", "communityId": cid, "amount": 3000, "summary": "购买三年期国债", "bondKind": "buy"})
	if resp["__error__"] != nil {
		t.Fatalf("购买国债失败: %v", resp)
	}
	// 到期兑付：本金 3000 + 利息 150
	resp = postJSON(t, createVoucher, "/api/vouchers", gin.H{
		"type": "bond", "date": "2026-09-10", "communityId": cid, "amount": 3000, "bondInterest": 150, "summary": "国债到期兑付", "bondKind": "redeem"})
	if resp["__error__"] != nil {
		t.Fatalf("到期兑付失败: %v", resp)
	}
	tx2, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := closeMonthTx(tx2, "2026-09", "test"); err != nil {
		t.Fatalf("closeMonthTx: %v", err)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatal(err)
	}
	// 国债投资清零
	if got := -subjectBalance(t, "110101", cid); got != 0 {
		t.Fatalf("110101 余额 = %d 分，want 0", got)
	}
	// 国债专户 = -3000 + 3150 = 150
	if got := -subjectBalance(t, "100103", cid); got != 15000 {
		t.Fatalf("100103 余额 = %d 分，want 15000", got)
	}
	// 待分配收益 = 国债利息 150
	if got := subjectBalance(t, "310101", cid); got != 15000 {
		t.Fatalf("310101 余额 = %d 分，want 15000", got)
	}
	// 兑付利息同步生成挂主凭证的"利息"业务凭证（计入公共账），不重复生成财务凭证
	var companion int
	db.QueryRow(`SELECT COUNT(*) FROM vouchers WHERE type='interest' AND master_id IS NOT NULL AND amount=15000`).Scan(&companion)
	if companion != 1 {
		t.Fatalf("应有 1 张国债利息业务凭证，got %d", companion)
	}
	// 公共账 = 国债利息 150
	if avail, err := communityPublicBalanceTx(db, cid); err != nil || avail != 15000 {
		t.Fatalf("公共账可分配余额 = %d（%v），want 15000", avail, err)
	}
	// 收支表：国债利息收入 150
	is := getJSON(t, glIncomeStatement, "/api/gl/income-statement?month=2026-09")
	found := false
	for _, r := range is["income"].([]interface{}) {
		m := r.(map[string]interface{})
		if m["name"] == "国债利息收入" && m["curTotal"].(float64) == 150 {
			found = true
		}
	}
	if !found {
		t.Fatalf("收支表国债利息收入应为 150：%v", is["income"])
	}
}

// ==================== 制单人 / 作废留痕 ====================

func TestCreatedBy(t *testing.T) {
	newTestDB(t)
	cid := seedCommunity(t, "制单人小区", "commercial")

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	id, err := glInsertTx(tx, "2026-09-01", "2026-09", "business", "manual", 0, "测试凭证",
		[]glEntry{
			{subject: "1201", project: cid, dir: "debit", amount: 100},
			{subject: "100101", project: cid, dir: "credit", amount: 100},
		}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var by string
	db.QueryRow(`SELECT created_by FROM gl_vouchers WHERE id=?`, id).Scan(&by)
	if by != "tester" {
		t.Fatalf("gl_vouchers.created_by = %q，want tester", by)
	}
}

// ==================== 月结财务报表快照 + 诊断 ====================

func TestStatementsSnapshot(t *testing.T) {
	newTestDB(t)
	cid := seedCommunity(t, "快照小区", "commercial")
	bid := seedBuilding(t, cid, "1栋")
	h1 := seedHousehold(t, bid, "101", 100, 1000000)

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	seedOpeningGL(t, tx, "2026-01-01", cid, "commercial", 1000000)
	seedBizVoucher(t, tx, "2026-09-05", "income", cid, h1, 100000, "", "缴存")
	if err := closeMonthTx(tx, "2026-09", "test"); err != nil {
		t.Fatalf("closeMonthTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	name, err := generateStatementsSnapshot("2026-09")
	if err != nil {
		t.Fatalf("generateStatementsSnapshot: %v", err)
	}
	if name != "财务报表-2026-09.xlsx" {
		t.Fatalf("快照文件名 = %q", name)
	}
	if _, err := os.Stat(filepath.Join(reportsDir, name)); err != nil {
		t.Fatalf("快照文件不存在: %v", err)
	}
	// 未月结月份有诊断提示
	diags := statementDiagnostics("2026-10")
	if len(diags) == 0 {
		t.Fatal("2026-10 未月结，诊断不应为空")
	}
	found := false
	for _, d := range diags {
		if d.Type == "unclosed" {
			found = true
		}
	}
	if !found {
		t.Fatalf("诊断应包含 unclosed：%v", diags)
	}
}

// ==================== PDF 输出 ====================

func TestStatementsPDF(t *testing.T) {
	newTestDB(t)
	cid := seedCommunity(t, "PDF小区", "commercial")
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	seedOpeningGL(t, tx, "2026-01-01", cid, "commercial", 1000000)
	tx.Commit()

	for _, tc := range []struct {
		target  string
		handler gin.HandlerFunc
	}{
		{"/api/gl/balance-sheet/pdf?month=2026-09", glBalanceSheetPDF},
		{"/api/gl/income-statement/pdf?month=2026-09", glIncomeStatementPDF},
		{"/api/gl/net-asset-statement/pdf?month=2026-09", glNetAssetStatementPDF},
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, tc.target, nil)
		tc.handler(c)
		ct := w.Header().Get("Content-Type")
		if ct != "application/pdf" {
			t.Fatalf("%s Content-Type = %q，want application/pdf", tc.target, ct)
		}
		body := w.Body.String()
		if !strings.HasPrefix(body, "%PDF") {
			t.Fatalf("%s 响应不是 PDF", tc.target)
		}
	}
}


func TestVoidVoucherTrail(t *testing.T) {
	newTestDB(t)
	cid := seedCommunity(t, "作废留痕小区", "commercial")
	bid := seedBuilding(t, cid, "1栋")
	h1 := seedHousehold(t, bid, "101", 100, 0)

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	seedBizVoucher(t, tx, "2026-09-05", "income", cid, h1, 100000, "", "缴存")
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var vid int64
	db.QueryRow(`SELECT id FROM vouchers WHERE type='income' LIMIT 1`).Scan(&vid)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("authUser", "tester")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/vouchers/"+itoa64(vid)+"/void",
		strings.NewReader(`{"reason":"录错金额"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: itoa64(vid)}}
	voidVoucher(c)
	if w.Code != http.StatusOK {
		t.Fatalf("作废应 200，got %d: %s", w.Code, w.Body.String())
	}
	// 业务凭证必须真的作废（回归：参数顺序错位曾导致 WHERE id=原因 匹配不到行）
	var status, by, reason string
	db.QueryRow(`SELECT status, voided_by, void_reason FROM vouchers WHERE id=?`, vid).Scan(&status, &by, &reason)
	if status != "voided" || by != "tester" || reason != "录错金额" {
		t.Fatalf("作废留痕 = (%q,%q,%q)，want (voided,tester,录错金额)", status, by, reason)
	}
	// 作废后汇总凭证重建：该户缴存对应的交存收入分录应消失
	var entCnt int
	db.QueryRow(`SELECT COUNT(*) FROM gl_entries WHERE subject_code='400101'`).Scan(&entCnt)
	if entCnt != 0 {
		t.Fatalf("作废后汇总凭证应交存分录清零，仍有 %d 条", entCnt)
	}
}

func itoa64(v int64) string {
	return strconv.FormatInt(v, 10)
}

// ==================== 无损益月份结转提示 ====================

func TestClosePeriodNoPL(t *testing.T) {
	newTestDB(t)
	cid := seedCommunity(t, "无损益小区", "commercial")
	bid := seedBuilding(t, cid, "1栋")
	h1 := seedHousehold(t, bid, "101", 100, 0)

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	seedBizVoucher(t, tx, "2026-06-01", "interest", cid, 0, 100000, "", "利息")
	seedInterestAlloc(t, tx, "2026-07-05", cid, 100000, map[int64]int64{h1: 100000})
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	// 7月只有收益分配（无损益）→ 结转提示无需结转
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/periods/close",
		strings.NewReader(`{"month":"2026-07"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	closePeriod(c)
	if !strings.Contains(w.Body.String(), "没有需要结转") {
		t.Fatalf("无损益月份结转应提示无需结转，got: %s", w.Body.String())
	}
	// 6月有损益 → 正常结转
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodPost, "/api/periods/close",
		strings.NewReader(`{"month":"2026-06"}`))
	c2.Request.Header.Set("Content-Type", "application/json")
	closePeriod(c2)
	if w2.Code != http.StatusOK {
		t.Fatalf("6月结转应 200，got %d: %s", w2.Code, w2.Body.String())
	}
	var cnt int
	db.QueryRow(`SELECT COUNT(*) FROM gl_vouchers WHERE kind='closing' AND month='2026-06'`).Scan(&cnt)
	if cnt != 1 {
		t.Fatalf("6月应有 1 张结转凭证，got %d", cnt)
	}
	// 重复结转幂等：再结转一次仍是 1 张（旧结转先清除再生成）
	w3 := httptest.NewRecorder()
	c3, _ := gin.CreateTestContext(w3)
	c3.Request = httptest.NewRequest(http.MethodPost, "/api/periods/close",
		strings.NewReader(`{"month":"2026-06"}`))
	c3.Request.Header.Set("Content-Type", "application/json")
	closePeriod(c3)
	if w3.Code != http.StatusOK {
		t.Fatalf("重复结转应 200，got %d: %s", w3.Code, w3.Body.String())
	}
	db.QueryRow(`SELECT COUNT(*) FROM gl_vouchers WHERE kind='closing' AND month='2026-06'`).Scan(&cnt)
	if cnt != 1 {
		t.Fatalf("重复结转后仍应 1 张结转凭证，got %d", cnt)
	}
}

// ==================== 作废留痕（参数顺序回归测试） ====================


func TestIncomeDuplicateGuard(t *testing.T) {
	newTestDB(t)
	cid := seedCommunity(t, "防重复小区", "commercial")
	bid := seedBuilding(t, cid, "1栋")
	h1 := seedHousehold(t, bid, "101", 100, 0)

	bodyStr := `{"type":"income","date":"2026-10-04","communityId":` +
		strconv.FormatInt(cid, 10) + `,"buildingId":` + strconv.FormatInt(bid, 10) +
		`,"householdId":` + strconv.FormatInt(h1, 10) + `,"amount":1300,"summary":"缴款方式：银行转账"}`
	doPost := func() int {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("authUser", "tester")
		c.Request = httptest.NewRequest(http.MethodPost, "/api/vouchers", strings.NewReader(bodyStr))
		c.Request.Header.Set("Content-Type", "application/json")
		createVoucher(c)
		return w.Code
	}
	if code := doPost(); code != http.StatusOK {
		t.Fatalf("首次保存应 200，got %d", code)
	}
	if code := doPost(); code != http.StatusConflict {
		t.Fatalf("10 分钟内重复提交应 409，got %d", code)
	}
	var cnt int
	db.QueryRow(`SELECT COUNT(*) FROM vouchers WHERE status='normal' AND community_id=?`, cid).Scan(&cnt)
	if cnt != 1 {
		t.Fatalf("应只有 1 张收款凭证，got %d", cnt)
	}
}

// ==================== 年度结转 / 反年度结转（无锁，改账后失效） ====================

func TestFiscalYearClose(t *testing.T) {
	newTestDB(t)
	cid := seedCommunity(t, "年结小区", "commercial")
	bid := seedBuilding(t, cid, "1栋")
	h1 := seedHousehold(t, bid, "101", 100, 0)

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	seedOpeningGL(t, tx, "2026-01-01", cid, "commercial", 0)
	seedBizVoucher(t, tx, "2026-01-05", "income", cid, h1, 100000, "", "缴存")
	seedBizVoucher(t, tx, "2026-01-06", "interest", cid, 0, 50000, "", "利息")
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	// 年度结转 2026：自动结转 1 月
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/periods/close-year",
		strings.NewReader(`{"year":"2026"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	closeFiscalYear(c)
	if w.Code != http.StatusOK {
		t.Fatalf("年度结转应 200，got %d: %s", w.Code, w.Body.String())
	}
	var cnt int
	db.QueryRow(`SELECT COUNT(*) FROM fiscal_years WHERE year='2026'`).Scan(&cnt)
	if cnt != 1 {
		t.Fatalf("fiscal_years 应有 2026，got %d", cnt)
	}
	db.QueryRow(`SELECT COUNT(*) FROM gl_vouchers WHERE kind='closing' AND month='2026-01'`).Scan(&cnt)
	if cnt != 1 {
		t.Fatalf("1月应已结转，got %d", cnt)
	}
	if _, err := os.Stat(filepath.Join(reportsDir, "财务报表-2026年度.xlsx")); err != nil {
		t.Fatalf("年度财务报表快照不存在: %v", err)
	}

	// 不锁账：年度结转后仍可记账；但改账会使该月结转与年度结转失效
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Set("authUser", "tester")
	c2.Request = httptest.NewRequest(http.MethodPost, "/api/vouchers",
		strings.NewReader(`{"type":"interest","date":"2026-01-10","communityId":`+strconv.FormatInt(cid, 10)+`,"amount":100,"summary":"补录"}`))
	c2.Request.Header.Set("Content-Type", "application/json")
	createVoucher(c2)
	if w2.Code != http.StatusOK {
		t.Fatalf("不锁账：结转后应可记账，got %d: %s", w2.Code, w2.Body.String())
	}
	db.QueryRow(`SELECT COUNT(*) FROM gl_vouchers WHERE kind='closing' AND month='2026-01'`).Scan(&cnt)
	if cnt != 0 {
		t.Fatalf("改账后 1 月结转应自动失效（清零），got %d", cnt)
	}
	db.QueryRow(`SELECT COUNT(*) FROM fiscal_years WHERE year='2026'`).Scan(&cnt)
	if cnt != 0 {
		t.Fatalf("改账后年度结转应自动失效，got %d", cnt)
	}

	// 反结转：删除结转凭证与结转记录
	tx2, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := closeMonthTx(tx2, "2026-01", "test"); err != nil {
		t.Fatalf("closeMonthTx: %v", err)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatal(err)
	}
	w3 := httptest.NewRecorder()
	c3, _ := gin.CreateTestContext(w3)
	c3.Request = httptest.NewRequest(http.MethodPost, "/api/periods/reopen",
		strings.NewReader(`{"month":"2026-01"}`))
	c3.Request.Header.Set("Content-Type", "application/json")
	reopenPeriod(c3)
	if w3.Code != http.StatusOK {
		t.Fatalf("反结转应 200，got %d: %s", w3.Code, w3.Body.String())
	}
	db.QueryRow(`SELECT COUNT(*) FROM gl_vouchers WHERE kind='closing' AND month='2026-01'`).Scan(&cnt)
	if cnt != 0 {
		t.Fatalf("反结转后结转凭证应清零，got %d", cnt)
	}
	db.QueryRow(`SELECT COUNT(*) FROM periods WHERE month='2026-01'`).Scan(&cnt)
	if cnt != 0 {
		t.Fatalf("反结转后结转记录应清零，got %d", cnt)
	}

	// 反年度结转：清除全年结转
	w4 := httptest.NewRecorder()
	c4, _ := gin.CreateTestContext(w4)
	c4.Request = httptest.NewRequest(http.MethodPost, "/api/periods/close-year",
		strings.NewReader(`{"year":"2026"}`))
	c4.Request.Header.Set("Content-Type", "application/json")
	closeFiscalYear(c4)
	w5 := httptest.NewRecorder()
	c5, _ := gin.CreateTestContext(w5)
	c5.Request = httptest.NewRequest(http.MethodPost, "/api/periods/reopen-year",
		strings.NewReader(`{"year":"2026"}`))
	c5.Request.Header.Set("Content-Type", "application/json")
	reopenFiscalYear(c5)
	if w5.Code != http.StatusOK {
		t.Fatalf("反年度结转应 200，got %d: %s", w5.Code, w5.Body.String())
	}
	db.QueryRow(`SELECT COUNT(*) FROM gl_vouchers WHERE kind='closing'`).Scan(&cnt)
	if cnt != 0 {
		t.Fatalf("反年度结转后结转凭证应清零，got %d", cnt)
	}
	db.QueryRow(`SELECT COUNT(*) FROM fiscal_years`).Scan(&cnt)
	if cnt != 0 {
		t.Fatalf("反年度结转后 fiscal_years 应清零，got %d", cnt)
	}
}

// ==================== 自定义会计科目 ====================

func TestCustomSubjects(t *testing.T) {
	newTestDB(t)
	cid := seedCommunity(t, "自定义科目小区", "commercial")

	// 新增一级收入科目 6101 专项服务收入 + 分栏子科目 610101/610102
	for _, body := range []gin.H{
		{"code": "6101", "name": "专项服务收入", "type": "income", "parent": ""},
		{"code": "610101", "name": "商品住宅专项服务收入", "type": "income", "parent": "6101"},
		{"code": "610102", "name": "公有住房专项服务收入", "type": "income", "parent": "6101"},
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/gl/subjects", strings.NewReader(toJSON(body)))
		c.Request.Header.Set("Content-Type", "application/json")
		glSubjectCreate(c)
		if w.Code != http.StatusOK {
			t.Fatalf("新增科目 %v 失败: %s", body, w.Body.String())
		}
	}
	// 记一笔分录到自定义科目
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := glInsertTx(tx, "2026-10-05", "2026-10", "business", "manual", 0, "自定义科目分录",
		[]glEntry{
			{subject: "100101", project: cid, dir: "debit", amount: 100000},
			{subject: "610101", project: cid, dir: "credit", amount: 100000},
		}, "test"); err != nil {
		t.Fatalf("glInsertTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	// 报表动态行：收支表出现"专项服务收入"，商品栏 1000
	is := getJSON(t, glIncomeStatement, "/api/gl/income-statement?month=2026-10")
	found := false
	for _, r := range is["income"].([]interface{}) {
		m := r.(map[string]interface{})
		if m["name"] == "专项服务收入" && m["curComm"].(float64) == 1000 && m["curTotal"].(float64) == 1000 {
			found = true
		}
	}
	if !found {
		t.Fatalf("收支表应包含自定义科目行（商品栏1000）：%v", is["income"])
	}
	// 改编码：6101 → 6201，分录与子科目同步迁移
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/gl/subjects/6101",
		strings.NewReader(`{"code":"6201"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "code", Value: "6101"}}
	glSubjectUpdate(c)
	if w.Code != http.StatusOK {
		t.Fatalf("改编码失败: %s", w.Body.String())
	}
	var cnt int
	db.QueryRow(`SELECT COUNT(*) FROM gl_entries WHERE subject_code='610101'`).Scan(&cnt)
	if cnt != 0 {
		t.Fatalf("旧编码分录应迁移，仍有 %d 条 610101", cnt)
	}
	db.QueryRow(`SELECT COUNT(*) FROM gl_entries WHERE subject_code='620101'`).Scan(&cnt)
	if cnt != 1 {
		t.Fatalf("新编码分录应 1 条，got %d", cnt)
	}
	var parent string
	db.QueryRow(`SELECT parent FROM gl_subjects WHERE code='620101'`).Scan(&parent)
	if parent != "6201" {
		t.Fatalf("子科目 parent 应为 6201，got %s", parent)
	}
	// 有分录的科目不能删除
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodDelete, "/api/gl/subjects/620101", nil)
	c2.Params = gin.Params{{Key: "code", Value: "620101"}}
	glSubjectDelete(c2)
	if w2.Code != http.StatusConflict {
		t.Fatalf("有分录科目删除应 409，got %d: %s", w2.Code, w2.Body.String())
	}
}

func toJSON(m gin.H) string {
	b, _ := json.Marshal(m)
	return string(b)
}
