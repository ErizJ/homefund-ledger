package app

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// ==================== 登录认证 ====================

func TestAuthLoginFlow(t *testing.T) {
	newTestDB(t)
	gin.SetMode(gin.TestMode)

	// 错误密码 → 401
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/login",
		strings.NewReader(`{"username":"admin","password":"wrong"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	authLogin(c)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("错误密码应 401，got %d", w.Code)
	}

	// 正确密码 → 200 + 会话 Cookie
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodPost, "/api/login",
		strings.NewReader(`{"username":"admin","password":"admin"}`))
	c2.Request.Header.Set("Content-Type", "application/json")
	authLogin(c2)
	if w2.Code != http.StatusOK {
		t.Fatalf("正确密码应 200，got %d: %s", w2.Code, w2.Body.String())
	}
	cookies := w2.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != "vf_token" {
		t.Fatalf("应设置 vf_token Cookie，got %v", cookies)
	}
	token := cookies[0].Value

	// 带 Cookie 访问受保护接口 → 放行（中间件与 handler 共享同一 gin.Context）
	w3 := httptest.NewRecorder()
	c3, _ := gin.CreateTestContext(w3)
	c3.Request = httptest.NewRequest(http.MethodGet, "/api/session", nil)
	c3.Request.AddCookie(cookies[0])
	authMiddleware()(c3)
	if w3.Code != http.StatusUnauthorized {
		authSession(c3)
	}
	if w3.Code != http.StatusOK {
		t.Fatalf("带 Cookie 应放行，got %d", w3.Code)
	}
	var m gin.H
	json.Unmarshal(w3.Body.Bytes(), &m)
	if m["user"] != "admin" {
		t.Fatalf("session user = %v，want admin", m["user"])
	}

	// 无 Cookie → 401
	w4 := httptest.NewRecorder()
	c4, _ := gin.CreateTestContext(w4)
	c4.Request = httptest.NewRequest(http.MethodGet, "/api/session", nil)
	authMiddleware()(c4)
	if w4.Code != http.StatusUnauthorized {
		t.Fatalf("无 Cookie 应 401，got %d", w4.Code)
	}

	// 注销后 Cookie 失效
	w5 := httptest.NewRecorder()
	c5, _ := gin.CreateTestContext(w5)
	c5.Request = httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	c5.Request.AddCookie(&http.Cookie{Name: "vf_token", Value: token})
	authLogout(c5)
	w6 := httptest.NewRecorder()
	c6, _ := gin.CreateTestContext(w6)
	c6.Request = httptest.NewRequest(http.MethodGet, "/api/session", nil)
	c6.Request.AddCookie(&http.Cookie{Name: "vf_token", Value: token})
	authMiddleware()(c6)
	if w6.Code != http.StatusUnauthorized {
		t.Fatalf("注销后应 401，got %d", w6.Code)
	}
}

// ==================== 会住维01表 资产负债表 ====================

func TestBalanceSheet(t *testing.T) {
	newTestDB(t)
	cid := seedCommunity(t, "报表小区", "commercial")
	bid := seedBuilding(t, cid, "1栋")
	h1 := seedHousehold(t, bid, "101", 100, 1000000)
	seedHousehold(t, bid, "102", 100, 1000000)

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	seedOpeningGL(t, tx, "2026-01-01", cid, "commercial", 2000000)
	seedBizVoucher(t, tx, "2026-09-05", "income", cid, h1, 1000000, "", "缴存")
	seedBizVoucher(t, tx, "2026-09-20", "interest", cid, 0, 50000, "", "利息")
	masterID := seedBizVoucher(t, tx, "2026-09-10", "expense", cid, 0, 300000, "engineering", "维修")
	seedAllocate(t, tx, "2026-09-10", cid, bid, h1, 150000, masterID)
	if err := closeMonthTx(tx, "2026-09", "test"); err != nil {
		t.Fatalf("closeMonthTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	resp := getJSON(t, glBalanceSheet, "/api/gl/balance-sheet?month=2026-09")
	if got, _ := resp["balanced"].(bool); !got {
		t.Fatalf("资产负债表应平衡：%v", resp)
	}
	// 资产 = 银行（期初20000+缴存10000+利息500-支出3000 = 27500 元）
	tot := resp["assetsTotalClosing"].(map[string]interface{})
	if got := tot["total"].(float64); got != 27500 {
		t.Fatalf("assetsTotalClosing.total = %v，want 27500（resp=%v）", got, resp)
	}
	// 全为商品住宅：商品栏 27500、公房栏 0
	if got := tot["comm"].(float64); got != 27500 || tot["pub"].(float64) != 0 {
		t.Fatalf("分栏错误 comm=%v pub=%v", tot["comm"], tot["pub"])
	}
	// 净资产 = 期初20000 + 缴存10000 - 支出3000 = 27000；待分配 = 500；合计 27500
	etot := resp["equityTotalClosing"].(map[string]interface{})
	if got := etot["total"].(float64); got != 27500 {
		t.Fatalf("equityTotalClosing.total = %v，want 27500", got)
	}
	// 年初余额 = 期初建账 20000（期初建账凭证无论日期均计入）
	otot := resp["assetsTotalOpening"].(map[string]interface{})
	if got := otot["total"].(float64); got != 20000 {
		t.Fatalf("assetsTotalOpening.total = %v，want 20000", got)
	}
	// 商品住宅维修资金行 = 27000 元
	equity := resp["equity"].([]interface{})
	found := false
	for _, r := range equity {
		m := r.(map[string]interface{})
		if m["name"] == "商品住宅维修资金" && m["closTotal"].(float64) == 27000 {
			found = true
		}
	}
	if !found {
		t.Fatalf("商品住宅维修资金行 closTotal 应为 27000：%v", equity)
	}
}

// ==================== 会住维02表 收支表 ====================

func TestIncomeStatement(t *testing.T) {
	newTestDB(t)
	cid := seedCommunity(t, "收支表小区", "commercial")
	bid := seedBuilding(t, cid, "1栋")
	h1 := seedHousehold(t, bid, "101", 100, 1000000)

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	seedOpeningGL(t, tx, "2026-01-01", cid, "commercial", 1000000)
	seedBizVoucher(t, tx, "2026-09-05", "income", cid, h1, 1000000, "", "缴存")
	seedBizVoucher(t, tx, "2026-09-20", "interest", cid, 0, 50000, "", "利息")
	seedRefund(t, tx, "2026-09-22", "return", cid, h1, 200000) // 退返冲减交存
	seedBizVoucher(t, tx, "2026-09-10", "expense", cid, 0, 300000, "engineering", "维修")
	seedBizVoucher(t, tx, "2026-09-25", "interest", cid, 0, 10000, "", "利息2")
	if err := closeMonthTx(tx, "2026-09", "test"); err != nil {
		t.Fatalf("closeMonthTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	resp := getJSON(t, glIncomeStatement, "/api/gl/income-statement?month=2026-09")
	// 交存收入净额 = 10000 - 2000（退返） = 8000
	if got := resp["income"].([]interface{})[0].(map[string]interface{})["curTotal"].(float64); got != 8000 {
		t.Fatalf("交存收入本月数 = %v，want 8000（resp=%v）", got, resp)
	}
	// 利息收入 = 500 + 100 = 600
	if got := resp["income"].([]interface{})[1].(map[string]interface{})["curTotal"].(float64); got != 600 {
		t.Fatalf("存款利息本月数 = %v，want 600", got)
	}
	// 维修支出 = 3000
	if got := resp["expense"].([]interface{})[0].(map[string]interface{})["curTotal"].(float64); got != 3000 {
		t.Fatalf("维修支出本月数 = %v，want 3000", got)
	}
	// 收支差额 = 8000 + 600 - 3000 = 5600（结转凭证不影响收支表）
	if got := resp["diff"].(map[string]interface{})["total"].(float64); got != 5600 {
		t.Fatalf("本期收支差额 = %v，want 5600", got)
	}
	// 本年累计 = 本月（仅 9 月有业务）
	if got := resp["diffCumulative"].(map[string]interface{})["total"].(float64); got != 5600 {
		t.Fatalf("累计收支差额 = %v，want 5600", got)
	}
	// 年度模式：上年数为 0
	resp2 := getJSON(t, glIncomeStatement, "/api/gl/income-statement?year=2026")
	if got := resp2["diffCumulative"].(map[string]interface{})["total"].(float64); got != 0 {
		t.Fatalf("年度模式上年差额 = %v，want 0", got)
	}
	if got := resp2["diff"].(map[string]interface{})["total"].(float64); got != 5600 {
		t.Fatalf("年度模式本年差额 = %v，want 5600", got)
	}
}

// ==================== 科目编码迁移（4103→4301 / 5201→5901） ====================

func TestMigrateSubjectCodes(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/old.db"
	d, err := openRaw(path)
	if err != nil {
		t.Fatal(err)
	}
	// 旧库：老编码科目 + 引用老编码的分录
	if _, err := d.Exec(schema); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Format(time.RFC3339)
	d.Exec(`INSERT INTO gl_subjects(code,name,type,parent) VALUES('4103','共用设施处置收入','income','')`)
	d.Exec(`INSERT INTO gl_subjects(code,name,type,parent) VALUES('410301','商品住宅处置收入','income','4103')`)
	d.Exec(`INSERT INTO gl_subjects(code,name,type,parent) VALUES('5201','其他支出','expense','')`)
	d.Exec(`INSERT INTO gl_subjects(code,name,type,parent) VALUES('520101','商品住宅其他支出','expense','5201')`)
	d.Exec(`INSERT INTO gl_vouchers(id,no,date,kind,source_type,summary,month,status,created_at) VALUES(1,'GL1','2026-09-01','business','manual','','2026-09','normal',?)`, now)
	d.Exec(`INSERT INTO gl_entries(voucher_id,subject_code,direction,amount) VALUES(1,'410301','credit',100)`)
	d.Exec(`INSERT INTO gl_entries(voucher_id,subject_code,direction,amount) VALUES(1,'520101','debit',50)`)
	d.Close()

	if err := openDB(path); err != nil {
		t.Fatalf("openDB（科目迁移）: %v", err)
	}
	var code string
	if err := db.QueryRow(`SELECT code FROM gl_subjects WHERE code='4301'`).Scan(&code); err != nil {
		t.Fatalf("4301 应存在：%v", err)
	}
	if err := db.QueryRow(`SELECT code FROM gl_subjects WHERE code='4103'`).Scan(&code); err == nil {
		t.Fatalf("4103 应已迁移，但仍在")
	}
	if err := db.QueryRow(`SELECT subject_code FROM gl_entries WHERE id=1 AND subject_code='430101'`).Scan(&code); err != nil {
		t.Fatalf("分录科目应迁移为 430101：%v", err)
	}
	if err := db.QueryRow(`SELECT parent FROM gl_subjects WHERE code='430101'`).Scan(&code); err != nil || code != "4301" {
		t.Fatalf("430101 parent 应为 4301，got %q (%v)", code, err)
	}
	// 新增科目（1201/4201/4901/5901）已预置
	for _, c := range []string{"1201", "4201", "4901", "5901"} {
		var cnt int
		db.QueryRow(`SELECT COUNT(*) FROM gl_subjects WHERE code=?`, c).Scan(&cnt)
		if cnt == 0 {
			t.Fatalf("科目 %s 应已预置", c)
		}
	}
	db.Close()
}

// openRaw 打开一个裸库文件（不经 openDB 迁移）
func openRaw(path string) (*sql.DB, error) {
	return sql.Open("sqlite", path)
}
