package main

import (
	"database/sql"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	_ "modernc.org/sqlite"
)

// 金额在库内一律以"分"为单位存储（int64），仅在 API/报表边界转换为"元"（float64）。
// 采用整数分避免浮点累计误差，符合资金类系统的业界惯例。
func yuanToCents(y float64) int64 { return int64(math.Round(y * 100)) }
func centsToYuan(c int64) float64 { return float64(c) / 100 }

var db *sql.DB

// uploadsDir 附件目录，跟随数据库文件所在目录，启动时自动创建
var uploadsDir string

const schema = `
CREATE TABLE IF NOT EXISTS communities (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE
);
CREATE TABLE IF NOT EXISTS buildings (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  community_id INTEGER NOT NULL REFERENCES communities(id),
  name TEXT NOT NULL,
  UNIQUE(community_id, name)
);
CREATE TABLE IF NOT EXISTS households (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  building_id INTEGER NOT NULL REFERENCES buildings(id),
  room_no TEXT NOT NULL,
  owner TEXT DEFAULT '',
  area REAL NOT NULL DEFAULT 0,
  opening_balance INTEGER NOT NULL DEFAULT 0,
  UNIQUE(building_id, room_no)
);
CREATE TABLE IF NOT EXISTS vouchers (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  no TEXT NOT NULL UNIQUE,
  date TEXT NOT NULL,
  type TEXT NOT NULL CHECK(type IN ('income','expense','interest','allocate')),
  community_id INTEGER NOT NULL REFERENCES communities(id),
  building_id INTEGER,
  household_id INTEGER,
  amount INTEGER NOT NULL,
  summary TEXT DEFAULT '',
  master_id INTEGER,
  status TEXT NOT NULL DEFAULT 'normal' CHECK(status IN ('normal','voided')),
  void_of INTEGER,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_v_household ON vouchers(household_id);
CREATE INDEX IF NOT EXISTS idx_v_community ON vouchers(community_id);
CREATE INDEX IF NOT EXISTS idx_v_date ON vouchers(date);

CREATE TABLE IF NOT EXISTS periods (
  month TEXT PRIMARY KEY,
  closed_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS attachments (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  voucher_id INTEGER NOT NULL REFERENCES vouchers(id),
  filename TEXT NOT NULL,
  stored_name TEXT NOT NULL,
  size INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_att_voucher ON attachments(voucher_id);
CREATE TABLE IF NOT EXISTS bank_txns (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  date TEXT NOT NULL,
  amount INTEGER NOT NULL,
  summary TEXT DEFAULT '',
  status TEXT NOT NULL DEFAULT 'unmatched' CHECK(status IN ('unmatched','matched','ignored')),
  matched_voucher_id INTEGER,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_bank_status ON bank_txns(status);

-- 财务账套（财会〔2020〕7号）
CREATE TABLE IF NOT EXISTS gl_subjects (
  code TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  type TEXT NOT NULL CHECK(type IN ('asset','liability','net_asset','income','expense')),
  parent TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE IF NOT EXISTS gl_vouchers (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  no TEXT NOT NULL UNIQUE,
  date TEXT NOT NULL,
  kind TEXT NOT NULL CHECK(kind IN ('business','closing','opening')),
  source_type TEXT NOT NULL DEFAULT '',
  source_id INTEGER NOT NULL DEFAULT 0,
  summary TEXT DEFAULT '',
  month TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'normal' CHECK(status IN ('normal','voided')),
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_gv_month ON gl_vouchers(month);
CREATE INDEX IF NOT EXISTS idx_gv_source ON gl_vouchers(source_type, source_id);
CREATE TABLE IF NOT EXISTS gl_entries (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  voucher_id INTEGER NOT NULL REFERENCES gl_vouchers(id),
  subject_code TEXT NOT NULL REFERENCES gl_subjects(code),
  project_id INTEGER,
  direction TEXT NOT NULL CHECK(direction IN ('debit','credit')),
  amount INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ge_voucher ON gl_entries(voucher_id);
CREATE INDEX IF NOT EXISTS idx_ge_subject ON gl_entries(subject_code);
CREATE INDEX IF NOT EXISTS idx_ge_project ON gl_entries(project_id);
`

// 增量迁移：老库补列（已存在时忽略报错），并将金额列从 REAL（元）迁到 INTEGER（分）
func migrate() error {
	stmts := []string{
		`ALTER TABLE communities ADD COLUMN fund_type TEXT NOT NULL DEFAULT 'commercial'`,
		`ALTER TABLE vouchers ADD COLUMN expense_category TEXT NOT NULL DEFAULT ''`,
	}
	for _, s := range stmts {
		db.Exec(s)
	}
	return migrateAmountsToCents()
}

// 金额列迁移需要重建的表：旧结构金额列为 REAL（元），新结构为 INTEGER（分）
type amountTableRebuild struct {
	name    string
	ddl     string // 新表 DDL（表名带 _new 后缀，结构与 schema 中一致）
	copy    string // 从旧表拷贝数据，金额列乘 100 后四舍五入为整数分
	indexes []string
}

var amountTables = []amountTableRebuild{
	{
		name: "vouchers",
		ddl: `CREATE TABLE vouchers_new (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  no TEXT NOT NULL UNIQUE,
  date TEXT NOT NULL,
  type TEXT NOT NULL CHECK(type IN ('income','expense','interest','allocate')),
  community_id INTEGER NOT NULL REFERENCES communities(id),
  building_id INTEGER,
  household_id INTEGER,
  amount INTEGER NOT NULL,
  summary TEXT DEFAULT '',
  master_id INTEGER,
  status TEXT NOT NULL DEFAULT 'normal' CHECK(status IN ('normal','voided')),
  void_of INTEGER,
  created_at TEXT NOT NULL,
  expense_category TEXT NOT NULL DEFAULT ''
)`,
		copy: `INSERT INTO vouchers_new (id,no,date,type,community_id,building_id,household_id,amount,summary,master_id,status,void_of,created_at,expense_category)
			SELECT id,no,date,type,community_id,building_id,household_id,CAST(ROUND(amount*100) AS INTEGER),summary,master_id,status,void_of,created_at,expense_category FROM vouchers`,
		indexes: []string{
			`CREATE INDEX idx_v_household ON vouchers(household_id)`,
			`CREATE INDEX idx_v_community ON vouchers(community_id)`,
			`CREATE INDEX idx_v_date ON vouchers(date)`,
		},
	},
	{
		name: "households",
		ddl: `CREATE TABLE households_new (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  building_id INTEGER NOT NULL REFERENCES buildings(id),
  room_no TEXT NOT NULL,
  owner TEXT DEFAULT '',
  area REAL NOT NULL DEFAULT 0,
  opening_balance INTEGER NOT NULL DEFAULT 0,
  UNIQUE(building_id, room_no)
)`,
		copy: `INSERT INTO households_new (id,building_id,room_no,owner,area,opening_balance)
			SELECT id,building_id,room_no,owner,area,CAST(ROUND(opening_balance*100) AS INTEGER) FROM households`,
	},
	{
		name: "bank_txns",
		ddl: `CREATE TABLE bank_txns_new (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  date TEXT NOT NULL,
  amount INTEGER NOT NULL,
  summary TEXT DEFAULT '',
  status TEXT NOT NULL DEFAULT 'unmatched' CHECK(status IN ('unmatched','matched','ignored')),
  matched_voucher_id INTEGER,
  created_at TEXT NOT NULL
)`,
		copy: `INSERT INTO bank_txns_new (id,date,amount,summary,status,matched_voucher_id,created_at)
			SELECT id,date,CAST(ROUND(amount*100) AS INTEGER),summary,status,matched_voucher_id,created_at FROM bank_txns`,
		indexes: []string{`CREATE INDEX idx_bank_status ON bank_txns(status)`},
	},
	{
		name: "gl_entries",
		ddl: `CREATE TABLE gl_entries_new (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  voucher_id INTEGER NOT NULL REFERENCES gl_vouchers(id),
  subject_code TEXT NOT NULL REFERENCES gl_subjects(code),
  project_id INTEGER,
  direction TEXT NOT NULL CHECK(direction IN ('debit','credit')),
  amount INTEGER NOT NULL
)`,
		copy: `INSERT INTO gl_entries_new (id,voucher_id,subject_code,project_id,direction,amount)
			SELECT id,voucher_id,subject_code,project_id,direction,CAST(ROUND(amount*100) AS INTEGER) FROM gl_entries`,
		indexes: []string{
			`CREATE INDEX idx_ge_voucher ON gl_entries(voucher_id)`,
			`CREATE INDEX idx_ge_subject ON gl_entries(subject_code)`,
			`CREATE INDEX idx_ge_project ON gl_entries(project_id)`,
		},
	},
}

// moneyColumnReal 判断表的金额列（amount / opening_balance）是否为 REAL；表不存在返回 false
func moneyColumnReal(table string) (bool, error) {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == "amount" || name == "opening_balance" {
			return strings.Contains(strings.ToUpper(typ), "REAL"), nil
		}
	}
	return false, rows.Err()
}

// migrateAmountsToCents 将旧库 REAL（元）金额列重建为 INTEGER（分）并转换存量数据。
// 重建期间单连接 + 关闭外键检查（SQLite ALTER 无法改列类型，只能重建表）。
func migrateAmountsToCents() error {
	var need []amountTableRebuild
	for _, t := range amountTables {
		real, err := moneyColumnReal(t.name)
		if err != nil {
			return fmt.Errorf("检查表 %s 失败: %w", t.name, err)
		}
		if real {
			need = append(need, t)
		}
	}
	if len(need) == 0 {
		return nil
	}
	// 迁移仅在启动时单线程执行：限制单连接，确保 PRAGMA 与事务作用于同一连接
	db.SetMaxOpenConns(1)
	defer db.SetMaxOpenConns(0)
	if _, err := db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		return err
	}
	defer db.Exec(`PRAGMA foreign_keys=ON`)

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, t := range need {
		if _, err := tx.Exec(t.ddl); err != nil {
			return fmt.Errorf("创建 %s_new 失败: %w", t.name, err)
		}
		if _, err := tx.Exec(t.copy); err != nil {
			return fmt.Errorf("转换 %s 金额失败: %w", t.name, err)
		}
		if _, err := tx.Exec(`DROP TABLE ` + t.name); err != nil {
			return fmt.Errorf("删除旧表 %s 失败: %w", t.name, err)
		}
		if _, err := tx.Exec(`ALTER TABLE ` + t.name + `_new RENAME TO ` + t.name); err != nil {
			return fmt.Errorf("重命名 %s 失败: %w", t.name, err)
		}
		for _, idx := range t.indexes {
			if _, err := tx.Exec(idx); err != nil {
				return fmt.Errorf("重建索引失败: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	log.Printf("金额列已迁移：%d 张表由元(REAL)转为分(INTEGER)", len(need))
	return nil
}

func openDB(path string) error {
	var err error
	db, err = sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	// 显式启用 WAL：WAL 模式下提交不创建/删除回滚日志（DELETE 模式每次提交需 unlink journal，
	// 在受限环境（如沙箱禁止 unlink）会导致 disk I/O error）
	if _, err = db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		return fmt.Errorf("启用 WAL 模式失败: %w", err)
	}
	if _, err = db.Exec(schema); err != nil {
		return fmt.Errorf("初始化表结构失败: %w", err)
	}
	if err = migrate(); err != nil {
		return fmt.Errorf("数据库迁移失败: %w", err)
	}
	seedGLSubjects()
	return nil
}

func main() {
	dbPath := "vfund.db"
	if len(os.Args) > 1 {
		dbPath = os.Args[1]
	}
	uploadsDir = filepath.Join(filepath.Dir(dbPath), "uploads")
	if err := os.MkdirAll(uploadsDir, 0o755); err != nil {
		log.Fatalf("创建附件目录失败: %v", err)
	}
	reportsDir = filepath.Join(filepath.Dir(dbPath), "reports")
	if err := os.MkdirAll(reportsDir, 0o755); err != nil {
		log.Fatalf("创建报表目录失败: %v", err)
	}
	if err := openDB(dbPath); err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}

	r := gin.Default()
	r.Use(corsMiddleware())

	api := r.Group("/api")
	{
		api.GET("/communities", listCommunities)
		api.POST("/communities", createCommunity)
		api.GET("/buildings", listBuildings)
		api.POST("/buildings", createBuilding)

		api.GET("/households", listHouseholds)
		api.POST("/households/import", importHouseholds)
		api.POST("/households", createHousehold)
		api.PUT("/households/:id", updateHousehold)
		api.GET("/households/:id/statement", householdStatement)
		api.DELETE("/households/:id", deleteHousehold)

		api.GET("/vouchers", listVouchers)
		api.GET("/vouchers/:id", voucherDetail)
		api.POST("/vouchers", createVoucher)
		api.POST("/vouchers/:id/void", voidVoucher)

		api.POST("/vouchers/:id/attachments", uploadAttachment)
		api.GET("/attachments/:id/download", downloadAttachment)
		api.DELETE("/attachments/:id", deleteAttachment)

		api.GET("/periods", listPeriods)
		api.POST("/periods/close", closePeriod)
		api.POST("/periods/reopen", reopenPeriod)

		api.POST("/bank/import", bankImport)
		api.GET("/bank/txns", bankList)
		api.GET("/bank/candidates", bankCandidates)
		api.POST("/bank/txns/:id/match", bankMatch)
		api.POST("/bank/txns/:id/ignore", bankIgnore)

		api.GET("/reports/households", reportHouseholds)
		api.GET("/reports/community-statement", reportCommunityStatement)
		api.GET("/reports/monthly", listMonthlyReports)
		api.POST("/reports/monthly", regenerateMonthlyReport)
		api.GET("/reports/monthly/file", downloadMonthlyReport)

		api.GET("/ledger/communities", ledgerCommunities)
		api.GET("/ledger/buildings", ledgerBuildings)
		api.GET("/ledger/households", ledgerHouseholds)

		api.GET("/stats/dashboard", statsDashboard)
		api.GET("/summary/daily", summaryDaily)
		api.GET("/summary/monthly", summaryMonthly)
		api.GET("/summary/yearly", summaryYearly)

		api.GET("/gl/subjects", glListSubjects)
		api.GET("/gl/balances", glBalances)
		api.GET("/gl/entries", glEntries)
		api.GET("/gl/trial-balance", glTrialBalance)
		api.GET("/gl/general-ledger", glGeneralLedger)
		api.GET("/gl/reconcile", glReconcile)
		api.POST("/gl/opening-balance", glOpeningBalance)
		api.GET("/gl/vouchers", glVoucherList)
		api.GET("/gl/vouchers/:id", glVoucherDetail)
		api.POST("/gl/manual-voucher", glManualVoucher)
		api.POST("/gl/vouchers/:id/void", glVoidManual)
		api.POST("/gl/backfill", glBackfill)
		api.POST("/gl/transfer", glTransferMonth)
	}

	log.Println("服务已启动: http://127.0.0.1:8080")
	os.MkdirAll("uploads", 0o755)
	if err := r.Run("127.0.0.1:8080"); err != nil {
		log.Fatalf("启动失败: %v", err)
	}
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
