package app

import (
	"database/sql"
	"fmt"
	"log"
	"math"
	"net/http"
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
  name TEXT NOT NULL UNIQUE,
  fund_type TEXT NOT NULL DEFAULT 'commercial',
  public_opening INTEGER NOT NULL DEFAULT 0,
  first_rate REAL NOT NULL DEFAULT 0
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
  type TEXT NOT NULL CHECK(type IN ('income','expense','interest','allocate','refund','interest_alloc','interest_alloc_child','fund_income','cash','bond')),
  community_id INTEGER NOT NULL REFERENCES communities(id),
  building_id INTEGER,
  household_id INTEGER,
  amount INTEGER NOT NULL,
  summary TEXT DEFAULT '',
  master_id INTEGER,
  status TEXT NOT NULL DEFAULT 'normal' CHECK(status IN ('normal','voided')),
  void_of INTEGER,
  created_at TEXT NOT NULL,
  created_by TEXT NOT NULL DEFAULT '',
  voided_by TEXT NOT NULL DEFAULT '',
  void_reason TEXT NOT NULL DEFAULT '',
  expense_category TEXT NOT NULL DEFAULT '',
  refund_kind TEXT NOT NULL DEFAULT '',
  biz_kind TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_v_household ON vouchers(household_id);
CREATE INDEX IF NOT EXISTS idx_v_community ON vouchers(community_id);
CREATE INDEX IF NOT EXISTS idx_v_date ON vouchers(date);

CREATE TABLE IF NOT EXISTS periods (
  month TEXT PRIMARY KEY,
  closed_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS fiscal_years (
  year TEXT PRIMARY KEY,
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
  appendix INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  created_by TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_gv_month ON gl_vouchers(month);
CREATE INDEX IF NOT EXISTS idx_gv_source ON gl_vouchers(source_type, source_id);
CREATE TABLE IF NOT EXISTS gl_entries (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  voucher_id INTEGER NOT NULL REFERENCES gl_vouchers(id),
  subject_code TEXT NOT NULL REFERENCES gl_subjects(code),
  project_id INTEGER,
  direction TEXT NOT NULL CHECK(direction IN ('debit','credit')),
  amount INTEGER NOT NULL,
  summary TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_ge_voucher ON gl_entries(voucher_id);
CREATE INDEX IF NOT EXISTS idx_ge_subject ON gl_entries(subject_code);
CREATE INDEX IF NOT EXISTS idx_ge_project ON gl_entries(project_id);
`

// 增量迁移：老库补列（已存在时忽略报错），并将金额列从 REAL（元）迁到 INTEGER（分）
func migrate() error {
	stmts := []string{
		`ALTER TABLE communities ADD COLUMN fund_type TEXT NOT NULL DEFAULT 'commercial'`,
		`ALTER TABLE communities ADD COLUMN public_opening INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE communities ADD COLUMN first_rate REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE vouchers ADD COLUMN expense_category TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE vouchers ADD COLUMN refund_kind TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE vouchers ADD COLUMN biz_kind TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE vouchers ADD COLUMN created_by TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE vouchers ADD COLUMN voided_by TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE vouchers ADD COLUMN void_reason TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE gl_entries ADD COLUMN summary TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE gl_vouchers ADD COLUMN appendix INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE gl_vouchers ADD COLUMN created_by TEXT NOT NULL DEFAULT ''`,
	}
	for _, s := range stmts {
		db.Exec(s)
	}
	if err := migrateAmountsToCents(); err != nil {
		return err
	}
	if err := migrateVoucherTypes(); err != nil {
		return err
	}
	if err := migrateSubjectCodes(); err != nil {
		return err
	}
	return nil
}

// migrateSubjectCodes 老库科目编码对齐财会〔2020〕7号附录：4103 共用设施处置收入→4301、5201 其他支出→5901。
// 同步改写 gl_entries 中的引用（需临时关闭外键检查），幂等。
func migrateSubjectCodes() error {
	renames := [][2]string{
		{"4103", "4301"}, {"410301", "430101"}, {"410302", "430102"},
		{"5201", "5901"}, {"520101", "590101"}, {"520102", "590102"},
	}
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
	for _, r := range renames {
		if _, err := tx.Exec(`UPDATE gl_entries SET subject_code=? WHERE subject_code=?`, r[1], r[0]); err != nil {
			return fmt.Errorf("迁移分录科目 %s→%s 失败: %w", r[0], r[1], err)
		}
		if _, err := tx.Exec(`UPDATE gl_subjects SET code=? WHERE code=?`, r[1], r[0]); err != nil {
			return fmt.Errorf("迁移科目 %s→%s 失败: %w", r[0], r[1], err)
		}
	}
	if _, err := tx.Exec(`UPDATE gl_subjects SET parent='4301' WHERE code IN ('430101','430102')`); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE gl_subjects SET parent='5901' WHERE code IN ('590101','590102')`); err != nil {
		return err
	}
	return tx.Commit()
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
  type TEXT NOT NULL CHECK(type IN ('income','expense','interest','allocate','refund','interest_alloc','interest_alloc_child','fund_income','cash','bond')),
  community_id INTEGER NOT NULL REFERENCES communities(id),
  building_id INTEGER,
  household_id INTEGER,
  amount INTEGER NOT NULL,
  summary TEXT DEFAULT '',
  master_id INTEGER,
  status TEXT NOT NULL DEFAULT 'normal' CHECK(status IN ('normal','voided')),
  void_of INTEGER,
  created_at TEXT NOT NULL,
  created_by TEXT NOT NULL DEFAULT '',
  voided_by TEXT NOT NULL DEFAULT '',
  void_reason TEXT NOT NULL DEFAULT '',
  expense_category TEXT NOT NULL DEFAULT '',
  refund_kind TEXT NOT NULL DEFAULT '',
  biz_kind TEXT NOT NULL DEFAULT ''
)`,
		copy: `INSERT INTO vouchers_new (id,no,date,type,community_id,building_id,household_id,amount,summary,master_id,status,void_of,created_at,created_by,voided_by,void_reason,expense_category,refund_kind,biz_kind)
			SELECT id,no,date,type,community_id,building_id,household_id,CAST(ROUND(amount*100) AS INTEGER),summary,master_id,status,void_of,created_at,created_by,voided_by,void_reason,expense_category,refund_kind,biz_kind FROM vouchers`,
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
  amount INTEGER NOT NULL,
  summary TEXT NOT NULL DEFAULT ''
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

// migrateVoucherTypes 老库 vouchers 表 CHECK 约束不含新凭证类型时重建表。
// 以最新类型标记 fund_income 判断；金额列已是 INTEGER（分），直接拷贝不换算；重建需关闭外键检查。
func migrateVoucherTypes() error {
	var ddl string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='vouchers'`).Scan(&ddl); err != nil {
		return nil // 表不存在（全新库首次建表即含新约束），无需迁移
	}
	if strings.Contains(ddl, "fund_income") {
		return nil
	}
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
	if _, err := tx.Exec(amountTables[0].ddl); err != nil {
		return fmt.Errorf("创建 vouchers_new 失败: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO vouchers_new (id,no,date,type,community_id,building_id,household_id,amount,summary,master_id,status,void_of,created_at,created_by,voided_by,void_reason,expense_category,refund_kind,biz_kind)
		SELECT id,no,date,type,community_id,building_id,household_id,amount,summary,master_id,status,void_of,created_at,created_by,voided_by,void_reason,expense_category,refund_kind,biz_kind FROM vouchers`); err != nil {
		return fmt.Errorf("拷贝 vouchers 失败: %w", err)
	}
	if _, err := tx.Exec(`DROP TABLE vouchers`); err != nil {
		return fmt.Errorf("删除旧 vouchers 失败: %w", err)
	}
	if _, err := tx.Exec(`ALTER TABLE vouchers_new RENAME TO vouchers`); err != nil {
		return fmt.Errorf("重命名 vouchers_new 失败: %w", err)
	}
	for _, idx := range amountTables[0].indexes {
		if _, err := tx.Exec(idx); err != nil {
			return fmt.Errorf("重建索引失败: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	log.Printf("vouchers 表已重建：凭证类型扩展（fund_income/cash/bond）")
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
	seedSettings()
	return nil
}

// seedSettings 预置系统设置（编制单位等）
func seedSettings() {
	db.Exec(`INSERT OR IGNORE INTO settings(key, value) VALUES('org_name', '演示代管单位')`)
	db.Exec(`INSERT OR IGNORE INTO settings(key, value) VALUES('auto_gl', '1')`)
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
