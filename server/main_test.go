package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

// newTestDB 打开一个独立的临时数据库（自动建表+迁移+预置科目），测试结束自动关闭。
func newTestDB(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	uploadsDir = filepath.Join(dir, "uploads")
	reportsDir = filepath.Join(dir, "reports")
	if err := os.MkdirAll(uploadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(reportsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := openDB(filepath.Join(dir, "test.db")); err != nil {
		t.Fatalf("openDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
}

func seedCommunity(t *testing.T, name, fundType string) int64 {
	t.Helper()
	res, err := db.Exec(`INSERT INTO communities(name, fund_type) VALUES(?,?)`, name, fundType)
	if err != nil {
		t.Fatalf("seedCommunity: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func seedBuilding(t *testing.T, communityID int64, name string) int64 {
	t.Helper()
	res, err := db.Exec(`INSERT INTO buildings(community_id, name) VALUES(?,?)`, communityID, name)
	if err != nil {
		t.Fatalf("seedBuilding: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func seedHousehold(t *testing.T, buildingID int64, room string, area float64, openingCents int64) int64 {
	t.Helper()
	res, err := db.Exec(`INSERT INTO households(building_id, room_no, owner, area, opening_balance) VALUES(?,?,?,?,?)`,
		buildingID, room, "户主"+room, area, openingCents)
	if err != nil {
		t.Fatalf("seedHousehold: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

var testVoucherSeq int64

func nextTestNo() string {
	return fmt.Sprintf("TEST-%d", atomic.AddInt64(&testVoucherSeq, 1))
}

// seedBizVoucher 在事务内插入一张业务凭证并生成对应财务分录（income 可指定户室），返回业务凭证 id。
func seedBizVoucher(t *testing.T, tx *sql.Tx, date, vtype string, communityID, householdID, amountCents int64, category, summary string) int64 {
	t.Helper()
	var household interface{}
	if householdID > 0 {
		household = householdID
	}
	res, err := tx.Exec(`INSERT INTO vouchers(no,date,type,community_id,household_id,amount,summary,expense_category,status,created_at)
		VALUES(?,?,?,?,?,?,?,?,'normal',?)`,
		nextTestNo(), date, vtype, communityID, household, amountCents, summary, category, time.Now().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("seedBizVoucher insert: %v", err)
	}
	id, _ := res.LastInsertId()
	if vtype == "income" || vtype == "interest" || vtype == "expense" {
		if err := generateBusinessGL(tx, bizGLReq{Type: vtype, Date: date, CommunityID: communityID,
			Amount: amountCents, Category: category, Summary: summary, BizID: id, CreatedBy: "test"}); err != nil {
			t.Fatalf("generateBusinessGL(%s): %v", vtype, err)
		}
	}
	return id
}

// seedAllocate 插入一张分摊子凭证（allocate 不产生财务分录）
func seedAllocate(t *testing.T, tx *sql.Tx, date string, communityID, buildingID, householdID, amountCents, masterID int64) {
	t.Helper()
	if _, err := tx.Exec(`INSERT INTO vouchers(no,date,type,community_id,building_id,household_id,amount,summary,master_id,status,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,'normal',?)`,
		nextTestNo(), date, "allocate", communityID, buildingID, householdID, amountCents, "分摊", masterID, time.Now().Format(time.RFC3339)); err != nil {
		t.Fatalf("seedAllocate: %v", err)
	}
}

// seedOpeningGL 按 glOpeningBalance 的口径生成期初建账财务凭证
func seedOpeningGL(t *testing.T, tx *sql.Tx, date string, communityID int64, fundType string, amountCents int64) {
	t.Helper()
	if _, err := glInsertTx(tx, date, date[:7], "opening", "opening", communityID,
		"期初建账", []glEntry{
			{subject: glSlot(fundType, "bank"), project: communityID, dir: "debit", amount: amountCents},
			{subject: glSlot(fundType, "netasset"), project: communityID, dir: "credit", amount: amountCents},
		}, "test"); err != nil {
		t.Fatalf("seedOpeningGL: %v", err)
	}
}

// subjectBalance 查询指定科目+小区的余额（贷正借负，单位分）
func subjectBalance(t *testing.T, subject string, projectID int64) int64 {
	t.Helper()
	var bal int64
	if err := db.QueryRow(`SELECT IFNULL(SUM(CASE e.direction WHEN 'credit' THEN e.amount ELSE -e.amount END),0)
		FROM gl_entries e JOIN gl_vouchers v ON e.voucher_id=v.id AND v.status='normal'
		WHERE e.subject_code=? AND e.project_id=?`, subject, projectID).Scan(&bal); err != nil {
		t.Fatalf("subjectBalance(%s): %v", subject, err)
	}
	return bal
}

// subjectMonthNet 查询某月内科目的净发生额（借正贷负，单位分），含该月结转凭证
func subjectMonthNet(t *testing.T, month, prefix string) int64 {
	t.Helper()
	var net int64
	if err := db.QueryRow(`SELECT IFNULL(SUM(CASE e.direction WHEN 'debit' THEN e.amount ELSE -e.amount END),0)
		FROM gl_entries e JOIN gl_vouchers v ON e.voucher_id=v.id AND v.status='normal' AND substr(v.date,1,7)=?
		JOIN gl_subjects s ON e.subject_code=s.code
		WHERE e.subject_code LIKE ?`, month, prefix).Scan(&net); err != nil {
		t.Fatalf("subjectMonthNet(%s, %s): %v", month, prefix, err)
	}
	return net
}

// ==================== splitByArea：按建筑面积分摊（单位分，尾差归末户） ====================

func TestSplitByArea(t *testing.T) {
	cases := []struct {
		name   string
		amount int64
		areas  []float64
		want   []int64
	}{
		{"等面积均摊尾差归末户", 10000, []float64{100, 100, 100}, []int64{3333, 3333, 3334}},
		{"按面积比例分摊", 10000, []float64{200, 100, 100}, []int64{5000, 2500, 2500}},
		{"单户全额分摊", 12345, []float64{88.88}, []int64{12345}},
		{"总面积为0时按户均摊", 100, []float64{0, 0, 0}, []int64{33, 33, 34}},
		{"空名单返回nil", 500, []float64{}, nil},
		{"1分钱分3户", 1, []float64{50, 50, 50}, []int64{0, 0, 1}},
		{"小数面积", 9999, []float64{88.5, 101.5}, []int64{4657, 5342}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitByArea(tc.amount, tc.areas)
			if len(got) != len(tc.want) {
				t.Fatalf("splitByArea(%d, %v) = %v, want %v", tc.amount, tc.areas, got, tc.want)
			}
			var sum int64
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("splitByArea(%d, %v) = %v, want %v", tc.amount, tc.areas, got, tc.want)
				}
				sum += got[i]
			}
			if len(tc.areas) > 0 && sum != tc.amount {
				t.Fatalf("分摊合计 %d 分 != 总额 %d 分", sum, tc.amount)
			}
		})
	}
}

// ==================== 旧库金额列迁移（REAL 元 → INTEGER 分） ====================

func TestMigrateAmountsToCents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "old.db")

	// 1. 构造旧版结构的库（金额列为 REAL 元）
	d, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	oldSchema := `
CREATE TABLE communities (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE, fund_type TEXT NOT NULL DEFAULT 'commercial');
CREATE TABLE buildings (id INTEGER PRIMARY KEY AUTOINCREMENT, community_id INTEGER NOT NULL, name TEXT NOT NULL, UNIQUE(community_id, name));
CREATE TABLE households (
  id INTEGER PRIMARY KEY AUTOINCREMENT, building_id INTEGER NOT NULL, room_no TEXT NOT NULL,
  owner TEXT DEFAULT '', area REAL NOT NULL DEFAULT 0, opening_balance REAL NOT NULL DEFAULT 0,
  UNIQUE(building_id, room_no));
CREATE TABLE vouchers (
  id INTEGER PRIMARY KEY AUTOINCREMENT, no TEXT NOT NULL UNIQUE, date TEXT NOT NULL,
  type TEXT NOT NULL, community_id INTEGER NOT NULL, building_id INTEGER, household_id INTEGER,
  amount REAL NOT NULL, summary TEXT DEFAULT '', master_id INTEGER,
  status TEXT NOT NULL DEFAULT 'normal', void_of INTEGER, created_at TEXT NOT NULL,
  expense_category TEXT NOT NULL DEFAULT '');
CREATE TABLE bank_txns (
  id INTEGER PRIMARY KEY AUTOINCREMENT, date TEXT NOT NULL, amount REAL NOT NULL,
  summary TEXT DEFAULT '', status TEXT NOT NULL DEFAULT 'unmatched',
  matched_voucher_id INTEGER, created_at TEXT NOT NULL);
CREATE TABLE gl_vouchers (
  id INTEGER PRIMARY KEY AUTOINCREMENT, no TEXT NOT NULL UNIQUE, date TEXT NOT NULL,
  kind TEXT NOT NULL, source_type TEXT NOT NULL DEFAULT '', source_id INTEGER NOT NULL DEFAULT 0,
  summary TEXT DEFAULT '', month TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'normal', created_at TEXT NOT NULL);
CREATE TABLE gl_entries (
  id INTEGER PRIMARY KEY AUTOINCREMENT, voucher_id INTEGER NOT NULL, subject_code TEXT NOT NULL,
  project_id INTEGER, direction TEXT NOT NULL, amount REAL NOT NULL);`
	if _, err := d.Exec(oldSchema); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Format(time.RFC3339)
	d.Exec(`INSERT INTO communities(id,name) VALUES(1,'旧小区')`)
	d.Exec(`INSERT INTO buildings(id,community_id,name) VALUES(1,1,'1栋')`)
	d.Exec(`INSERT INTO households(id,building_id,room_no,area,opening_balance) VALUES(1,1,'101',88.88,100.0)`)
	d.Exec(`INSERT INTO vouchers(id,no,date,type,community_id,household_id,amount,created_at) VALUES(1,'PZ1','2026-09-01','income',1,1,123.45,?)`, now)
	d.Exec(`INSERT INTO vouchers(id,no,date,type,community_id,amount,created_at) VALUES(2,'PZ2','2026-09-02','expense',1,24.0,?)`, now)
	d.Exec(`INSERT INTO bank_txns(id,date,amount,created_at) VALUES(1,'2026-09-03',-0.1,?)`, now)
	d.Exec(`INSERT INTO gl_vouchers(id,no,date,kind,month,created_at) VALUES(1,'GL1','2026-09-01','business','2026-09',?)`, now)
	d.Exec(`INSERT INTO gl_entries(id,voucher_id,subject_code,direction,amount) VALUES(1,1,'1001','debit',0.01)`)
	d.Close()

	// 2. 用 openDB 打开触发迁移
	if err := openDB(path); err != nil {
		t.Fatalf("openDB（迁移）: %v", err)
	}

	assert := func() {
		t.Helper()
		var v int64
		if err := db.QueryRow(`SELECT amount FROM vouchers WHERE id=1`).Scan(&v); err != nil || v != 12345 {
			t.Fatalf("vouchers#1 amount = %d (%v), want 12345", v, err)
		}
		if err := db.QueryRow(`SELECT amount FROM vouchers WHERE id=2`).Scan(&v); err != nil || v != 2400 {
			t.Fatalf("vouchers#2 amount = %d (%v), want 2400", v, err)
		}
		if err := db.QueryRow(`SELECT opening_balance FROM households WHERE id=1`).Scan(&v); err != nil || v != 10000 {
			t.Fatalf("households#1 opening = %d (%v), want 10000", v, err)
		}
		if err := db.QueryRow(`SELECT amount FROM bank_txns WHERE id=1`).Scan(&v); err != nil || v != -10 {
			t.Fatalf("bank_txns#1 amount = %d (%v), want -10", v, err)
		}
		if err := db.QueryRow(`SELECT amount FROM gl_entries WHERE id=1`).Scan(&v); err != nil || v != 1 {
			t.Fatalf("gl_entries#1 amount = %d (%v), want 1", v, err)
		}
	}
	assert()
	db.Close()

	// 3. 再次打开：迁移幂等，数据不被二次转换
	if err := openDB(path); err != nil {
		t.Fatalf("openDB（二次）: %v", err)
	}
	assert()
	db.Close()
}
