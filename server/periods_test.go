package main

import (
	"errors"
	"testing"
)

// ==================== 月结结转（closeMonthTx / reopenMonthTx） ====================

// TestCloseMonthTx 表驱动：验证结转分录生成、损益清零、净资产/待分配收益归集与锁账。
func TestCloseMonthTx(t *testing.T) {
	cases := []struct {
		name string
		fund string // commercial | public
		// 期望：结转后 净资产/待分配/银行 科目余额（分）
		wantNetAsset string // 3001（商）或 3002（公）
		wantPending  string // 310101（商）或 310102（公）
		wantBank     string // 100101（商）或 100102（公）
		wantNet      int64
		wantPendingB int64
		wantBankB    int64
	}{
		{
			name: "商业小区：缴存+维修支出+利息",
			fund: "commercial",
			wantNetAsset: "3001", wantPending: "310101", wantBank: "100101",
			// 期初 70000 元 + 收入 150000 元 - 支出 50000 元 = 170000 元
			wantNet: 17000000,
			// 利息 500 元入待分配收益
			wantPendingB: 50000,
			// 期初 70000 + 收入 150000 + 利息 500 - 支出 50000 = 170500 元
			wantBankB: 17050000,
		},
		{
			name: "公有住房：结转路由到公房科目",
			fund: "public",
			wantNetAsset: "3002", wantPending: "310102", wantBank: "100102",
			// 期初 70000 元（公房期初建账）+ 收入 100000 元 = 170000 元 → 3002
			wantNet: 17000000,
			// 利息 200 元 → 310102
			wantPendingB: 20000,
			// 期初 70000 + 收入 100000 + 利息 200 = 170200 元
			wantBankB: 17020000,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			newTestDB(t)
			cid := seedCommunity(t, tc.name, tc.fund)
			bid := seedBuilding(t, cid, "1栋")
			h1 := seedHousehold(t, bid, "101", 100, 5000000) // 期初 50000 元
			h2 := seedHousehold(t, bid, "102", 100, 2000000) // 期初 20000 元

			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			// 期初建账财务凭证（借银行 / 贷净资产）
			seedOpeningGL(t, tx, "2026-01-01", cid, tc.fund, 7000000)

			if tc.fund == "commercial" {
				seedBizVoucher(t, tx, "2026-09-05", "income", cid, h1, 10000000, "", "缴存")
				seedBizVoucher(t, tx, "2026-09-05", "income", cid, h2, 5000000, "", "缴存")
				masterID := seedBizVoucher(t, tx, "2026-09-10", "expense", cid, 0, 5000000, "engineering", "电梯大修")
				seedAllocate(t, tx, "2026-09-10", cid, bid, h1, 2500000, masterID)
				seedAllocate(t, tx, "2026-09-10", cid, bid, h2, 2500000, masterID)
				seedBizVoucher(t, tx, "2026-09-20", "interest", cid, 0, 50000, "", "存款利息")
			} else {
				seedBizVoucher(t, tx, "2026-09-05", "income", cid, h1, 10000000, "", "缴存")
				seedBizVoucher(t, tx, "2026-09-20", "interest", cid, 0, 20000, "", "存款利息")
			}
			if err := closeMonthTx(tx, "2026-09", "test"); err != nil {
				t.Fatalf("closeMonthTx: %v", err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatalf("commit: %v", err)
			}

			// 锁账记录已写入
			var cnt int
			db.QueryRow(`SELECT COUNT(*) FROM periods WHERE month='2026-09'`).Scan(&cnt)
			if cnt != 1 {
				t.Fatalf("periods 应含 2026-09 锁账记录，got %d", cnt)
			}
			// 结转凭证存在且仅一张
			db.QueryRow(`SELECT COUNT(*) FROM gl_vouchers WHERE kind='closing' AND month='2026-09'`).Scan(&cnt)
			if cnt != 1 {
				t.Fatalf("结转凭证应为 1 张，got %d", cnt)
			}
			// 本月损益清零（业务+结转分录净额 = 0）
			if n := subjectMonthNet(t, "2026-09", "4%"); n != 0 {
				t.Fatalf("结转后 4 类科目本月净额应为 0，got %d", n)
			}
			if n := subjectMonthNet(t, "2026-09", "5%"); n != 0 {
				t.Fatalf("结转后 5 类科目本月净额应为 0，got %d", n)
			}
			// 净资产 / 待分配收益 / 银行余额
			if got := subjectBalance(t, tc.wantNetAsset, cid); got != tc.wantNet {
				t.Fatalf("%s 余额 = %d 分，want %d 分", tc.wantNetAsset, got, tc.wantNet)
			}
			if got := subjectBalance(t, tc.wantPending, cid); got != tc.wantPendingB {
				t.Fatalf("%s 余额 = %d 分，want %d 分", tc.wantPending, got, tc.wantPendingB)
			}
			// 银行存款为借方余额科目，subjectBalance 按"贷正借负"计，故取反
			if got := -subjectBalance(t, tc.wantBank, cid); got != tc.wantBankB {
				t.Fatalf("%s 余额 = %d 分，want %d 分", tc.wantBank, got, tc.wantBankB)
			}
		})
	}
}

// TestCloseMonthTxNoActivity 无损益发生额时返回 errNothingToClose，且不产生锁账。
func TestCloseMonthTxNoActivity(t *testing.T) {
	newTestDB(t)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := closeMonthTx(tx, "2026-08", "test"); !errors.Is(err, errNothingToClose) {
		t.Fatalf("closeMonthTx 无发生额应返回 errNothingToClose，got %v", err)
	}
}

// TestCloseMonthTxIdempotent 重复结转幂等：清除旧结转后按最新账目重新生成，仅保留 1 张结转凭证
func TestCloseMonthTxIdempotent(t *testing.T) {
	newTestDB(t)
	cid := seedCommunity(t, "重复结转", "commercial")
	bid := seedBuilding(t, cid, "1栋")
	h1 := seedHousehold(t, bid, "101", 100, 0)

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	seedBizVoucher(t, tx, "2026-09-05", "income", cid, h1, 1000000, "", "缴存")
	if err := closeMonthTx(tx, "2026-09", "test"); err != nil {
		t.Fatalf("closeMonthTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	tx2, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx2.Rollback()
	if err := closeMonthTx(tx2, "2026-09", "test"); err != nil {
		t.Fatalf("重复结转应幂等成功: %v", err)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatal(err)
	}
	var cnt int
	db.QueryRow(`SELECT COUNT(*) FROM gl_vouchers WHERE kind='closing' AND month='2026-09'`).Scan(&cnt)
	if cnt != 1 {
		t.Fatalf("重复结转后结转凭证应为 1 张，got %d", cnt)
	}
	db.QueryRow(`SELECT COUNT(*) FROM periods WHERE month='2026-09'`).Scan(&cnt)
	if cnt != 1 {
		t.Fatalf("periods 记录应为 1 条，got %d", cnt)
	}
}

// TestReopenMonthTx 反结转：解除锁账、删除结转凭证、损益发生额恢复。
func TestReopenMonthTx(t *testing.T) {
	newTestDB(t)
	cid := seedCommunity(t, "反结转", "commercial")
	bid := seedBuilding(t, cid, "1栋")
	h1 := seedHousehold(t, bid, "101", 100, 0)

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	seedBizVoucher(t, tx, "2026-09-05", "income", cid, h1, 10000000, "", "缴存")
	seedBizVoucher(t, tx, "2026-09-10", "expense", cid, 0, 5000000, "engineering", "维修")
	if err := closeMonthTx(tx, "2026-09", "test"); err != nil {
		t.Fatalf("closeMonthTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	tx2, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := reopenMonthTx(tx2, "2026-09"); err != nil {
		t.Fatalf("reopenMonthTx: %v", err)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatal(err)
	}

	var cnt int
	db.QueryRow(`SELECT COUNT(*) FROM periods WHERE month='2026-09'`).Scan(&cnt)
	if cnt != 0 {
		t.Fatalf("反结转后 periods 应无 2026-09，got %d", cnt)
	}
	db.QueryRow(`SELECT COUNT(*) FROM gl_vouchers WHERE kind='closing' AND month='2026-09'`).Scan(&cnt)
	if cnt != 0 {
		t.Fatalf("反结转后结转凭证应为 0，got %d", cnt)
	}
	// 损益发生额恢复：交存收入贷 100000 元（净额 -10000000 分）
	if n := subjectMonthNet(t, "2026-09", "400101"); n != -10000000 {
		t.Fatalf("反结转后 400101 净额 = %d，want -10000000", n)
	}
	// 维修支出借 50000 元
	if n := subjectMonthNet(t, "2026-09", "500101"); n != 5000000 {
		t.Fatalf("反结转后 500101 净额 = %d，want 5000000", n)
	}
}
