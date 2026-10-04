package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// getJSON 用测试上下文直接调用 GET 处理器并解析 JSON 响应。
func getJSON(t *testing.T, handler gin.HandlerFunc, target string) gin.H {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, target, nil)
	handler(c)
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s → %d, body: %s", target, w.Code, w.Body.String())
	}
	var m gin.H
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("解析 %s 响应失败: %v", target, err)
	}
	return m
}

// ==================== 试算平衡 ====================

func TestTrialBalance(t *testing.T) {
	cases := []struct {
		name         string
		setup        func(t *testing.T, cid int64)
		month        string
		wantBalanced bool
		wantDebit    float64 // 元
		wantCredit   float64
	}{
		{
			name: "平衡凭证",
			setup: func(t *testing.T, cid int64) {
				tx, err := db.Begin()
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback()
				if _, err := glInsertTx(tx, "2026-09-15", "2026-09", "business", "manual", 0, "手工", []glEntry{
					{subject: "100101", project: cid, dir: "debit", amount: 100000},
					{subject: "3001", project: cid, dir: "credit", amount: 100000},
				}, "test"); err != nil {
					t.Fatalf("glInsertTx: %v", err)
				}
				tx.Commit()
			},
			month: "2026-09", wantBalanced: true, wantDebit: 1000.0, wantCredit: 1000.0,
		},
		{
			name: "不平分录检出差异",
			setup: func(t *testing.T, cid int64) {
				res, err := db.Exec(`INSERT INTO gl_vouchers(no,date,kind,source_type,source_id,summary,month,status,created_at)
					VALUES('RAW-1','2026-09-16','business','manual',0,'不平','2026-09','normal',?)`,
					time.Now().Format(time.RFC3339))
				if err != nil {
					t.Fatal(err)
				}
				vid, _ := res.LastInsertId()
				db.Exec(`INSERT INTO gl_entries(voucher_id,subject_code,project_id,direction,amount) VALUES(?,?,?,?,?)`,
					vid, "100101", cid, "debit", 99999)
				db.Exec(`INSERT INTO gl_entries(voucher_id,subject_code,project_id,direction,amount) VALUES(?,?,?,?,?)`,
					vid, "3001", cid, "credit", 100000)
			},
			month: "2026-09", wantBalanced: false, wantDebit: 999.99, wantCredit: 1000.0,
		},
		{
			name: "月份过滤：其他月份不计入",
			setup: func(t *testing.T, cid int64) {
				tx, err := db.Begin()
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback()
				if _, err := glInsertTx(tx, "2026-09-15", "2026-09", "business", "manual", 0, "九月", []glEntry{
					{subject: "100101", project: cid, dir: "debit", amount: 100000},
					{subject: "3001", project: cid, dir: "credit", amount: 100000},
				}, "test"); err != nil {
					t.Fatalf("glInsertTx(9月): %v", err)
				}
				if _, err := glInsertTx(tx, "2026-10-05", "2026-10", "business", "manual", 0, "十月", []glEntry{
					{subject: "100101", project: cid, dir: "debit", amount: 50000},
					{subject: "3001", project: cid, dir: "credit", amount: 50000},
				}, "test"); err != nil {
					t.Fatalf("glInsertTx(10月): %v", err)
				}
				tx.Commit()
			},
			month: "2026-09", wantBalanced: true, wantDebit: 1000.0, wantCredit: 1000.0,
		},
		{
			name: "作废凭证不计入",
			setup: func(t *testing.T, cid int64) {
				tx, err := db.Begin()
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback()
				if _, err := glInsertTx(tx, "2026-09-15", "2026-09", "business", "manual", 0, "有效", []glEntry{
					{subject: "100101", project: cid, dir: "debit", amount: 100000},
					{subject: "3001", project: cid, dir: "credit", amount: 100000},
				}, "test"); err != nil {
					t.Fatalf("glInsertTx: %v", err)
				}
				tx.Commit()
				// 作废该月全部财务凭证
				if _, err := db.Exec(`UPDATE gl_vouchers SET status='voided' WHERE month='2026-09'`); err != nil {
					t.Fatal(err)
				}
			},
			month: "2026-09", wantBalanced: true, wantDebit: 0, wantCredit: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			newTestDB(t)
			cid := seedCommunity(t, tc.name, "commercial")
			tc.setup(t, cid)
			resp := getJSON(t, glTrialBalance, "/api/gl/trial-balance?month="+tc.month)
			if got, _ := resp["balanced"].(bool); got != tc.wantBalanced {
				t.Fatalf("balanced = %v, want %v", got, tc.wantBalanced)
			}
			if got := resp["debit"].(float64); got != tc.wantDebit {
				t.Fatalf("debit = %v, want %v", got, tc.wantDebit)
			}
			if got := resp["credit"].(float64); got != tc.wantCredit {
				t.Fatalf("credit = %v, want %v", got, tc.wantCredit)
			}
		})
	}
}

// ==================== 财务账 ↔ 业务台账双线对账 ====================

func reconcileRow(t *testing.T, resp gin.H, communityID int64) map[string]interface{} {
	t.Helper()
	rows, ok := resp["rows"].([]interface{})
	if !ok {
		t.Fatalf("对账响应缺少 rows: %v", resp)
	}
	for _, r := range rows {
		m := r.(map[string]interface{})
		if int64(m["communityId"].(float64)) == communityID {
			return m
		}
	}
	t.Fatalf("对账结果中没有小区 %d: %v", communityID, resp)
	return nil
}

func TestReconcile(t *testing.T) {
	cases := []struct {
		name            string
		setup           func(t *testing.T, cid, bid, h1, h2 int64)
		wantOpeningDone bool
		wantNetDiff     float64 // 元
		wantPendingDiff float64
		wantGlBank      float64
		wantBizHouse    float64
	}{
		{
			name: "未建财务账：差额=业务台账余额",
			setup: func(t *testing.T, cid, bid, h1, h2 int64) {
				// 只有业务台账（期初建账），无任何财务凭证
			},
			wantOpeningDone: false,
			wantNetDiff:     -100000.0, // glNet(0) - bizHousehold(100000)
			wantPendingDiff: 0,
			wantGlBank:      0,
			wantBizHouse:    100000.0,
		},
		{
			name: "建账+业务+月结后：双线一致",
			setup: func(t *testing.T, cid, bid, h1, h2 int64) {
				tx, err := db.Begin()
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback()
				seedOpeningGL(t, tx, "2026-01-01", cid, "commercial", 10000000)
				seedBizVoucher(t, tx, "2026-09-05", "income", cid, h1, 10000000, "", "缴存")
				masterID := seedBizVoucher(t, tx, "2026-09-10", "expense", cid, 0, 5000000, "engineering", "维修")
				seedAllocate(t, tx, "2026-09-10", cid, bid, h1, 2500000, masterID)
				seedAllocate(t, tx, "2026-09-10", cid, bid, h2, 2500000, masterID)
				seedBizVoucher(t, tx, "2026-09-20", "interest", cid, 0, 50000, "", "利息")
				if err := closeMonthTx(tx, "2026-09", "test"); err != nil {
					t.Fatalf("closeMonthTx: %v", err)
				}
				tx.Commit()
			},
			wantOpeningDone: true,
			wantNetDiff:     0,
			wantPendingDiff: 0,
			// 银行 = 期初 100000 + 收入 100000 + 利息 500 - 支出 50000 = 150500
			wantGlBank:   150500.0,
			wantBizHouse: 150000.0,
		},
		{
			name: "作废凭证双线同步剔除",
			setup: func(t *testing.T, cid, bid, h1, h2 int64) {
				tx, err := db.Begin()
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback()
				seedOpeningGL(t, tx, "2026-01-01", cid, "commercial", 10000000)
				voidedID := seedBizVoucher(t, tx, "2026-09-05", "income", cid, h2, 5000000, "", "缴存（将作废）")
				seedBizVoucher(t, tx, "2026-09-05", "income", cid, h1, 10000000, "", "缴存")
				masterID := seedBizVoucher(t, tx, "2026-09-10", "expense", cid, 0, 5000000, "engineering", "维修")
				seedAllocate(t, tx, "2026-09-10", cid, bid, h1, 2500000, masterID)
				seedAllocate(t, tx, "2026-09-10", cid, bid, h2, 2500000, masterID)
				seedBizVoucher(t, tx, "2026-09-20", "interest", cid, 0, 50000, "", "利息")
				// 作废业务凭证并重建当日汇总凭证（与 voidVoucher 口径一致）
				tx.Exec(`UPDATE vouchers SET status='voided' WHERE id=?`, voidedID)
				if err := rebuildCommunityDayGL(tx, cid, "2026-09-05", "test", false); err != nil {
					t.Fatalf("rebuildCommunityDayGL: %v", err)
				}
				if err := closeMonthTx(tx, "2026-09", "test"); err != nil {
					t.Fatalf("closeMonthTx: %v", err)
				}
				tx.Commit()
			},
			wantOpeningDone: true,
			wantNetDiff:     0,
			wantPendingDiff: 0,
			// 银行 = 期初 100000 + 收入 100000 + 利息 500 - 支出 50000 = 150500
			wantGlBank:   150500.0,
			wantBizHouse: 150000.0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			newTestDB(t)
			cid := seedCommunity(t, tc.name, "commercial")
			bid := seedBuilding(t, cid, "1栋")
			h1 := seedHousehold(t, bid, "101", 100, 5000000)
			h2 := seedHousehold(t, bid, "102", 100, 5000000)
			tc.setup(t, cid, bid, h1, h2)

			resp := getJSON(t, glReconcile, "/api/gl/reconcile?month=2026-09")
			row := reconcileRow(t, resp, cid)
			if got, _ := row["openingDone"].(bool); got != tc.wantOpeningDone {
				t.Fatalf("openingDone = %v, want %v", got, tc.wantOpeningDone)
			}
			if got := row["netDiff"].(float64); got != tc.wantNetDiff {
				t.Fatalf("netDiff = %v, want %v（glNet=%v bizHousehold=%v）", got, tc.wantNetDiff, row["glNetAsset"], row["bizHousehold"])
			}
			if got := row["pendingDiff"].(float64); got != tc.wantPendingDiff {
				t.Fatalf("pendingDiff = %v, want %v", got, tc.wantPendingDiff)
			}
			if got := row["glBank"].(float64); got != tc.wantGlBank {
				t.Fatalf("glBank = %v, want %v", got, tc.wantGlBank)
			}
			if got := row["bizHousehold"].(float64); got != tc.wantBizHouse {
				t.Fatalf("bizHousehold = %v, want %v", got, tc.wantBizHouse)
			}
		})
	}
}
