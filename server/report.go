package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

// reportsDir 月报表目录，跟随数据库文件所在目录，启动时自动创建
var reportsDir string

// ==================== 数据汇总（业务台账口径：户账 + 公共账） ====================

func sumVouchers(where string, args ...interface{}) int64 {
	var s int64
	db.QueryRow(`SELECT IFNULL(SUM(amount),0) FROM vouchers WHERE status='normal' AND `+where, args...).Scan(&s)
	return s
}

// monthAgg 一个月（或任一日期区间）的资金汇总，金额单位为分
type monthAgg struct {
	opening        int64
	income         int64
	interest       int64
	fundIncome     int64
	refundReturn   int64
	refundDestroy  int64
	expEngineering int64
	expSupervision int64
	expDetection   int64
	expOther       int64
}

// collectAgg 汇总 [from, to] 区间的发生额与期初（期初 = date < from 的累计）。
// 口径：户账（期初建账+缴存+利息分配-分摊-返还）+ 公共账（公共账期初+利息+其他收入-已分配收益）= 基金总额；
// 收益分配为公共账→户账的内部结转，汇总层面相互抵消。
func collectAgg(from, to string) monthAgg {
	var a monthAgg
	// 期初 = 户账期初 + 公共账期初 + 期前缴存 - 期前分摊 + 期前利息 + 期前其他收入 - 期前返还
	var openSum, publicOpenSum int64
	db.QueryRow(`SELECT IFNULL(SUM(opening_balance),0) FROM households`).Scan(&openSum)
	db.QueryRow(`SELECT IFNULL(SUM(public_opening),0) FROM communities`).Scan(&publicOpenSum)
	incB := sumVouchers("type='income' AND date < ?", from)
	allocB := sumVouchers("type='allocate' AND date < ?", from)
	intB := sumVouchers("type='interest' AND date < ?", from)
	fiB := sumVouchers("type='fund_income' AND date < ?", from)
	refundB := sumVouchers("type='refund' AND date < ?", from)
	a.opening = openSum + publicOpenSum + incB - allocB + intB + fiB - refundB
	// 本期收入
	a.income = sumVouchers("type='income' AND date >= ? AND date <= ?", from, to)
	a.interest = sumVouchers("type='interest' AND date >= ? AND date <= ?", from, to)
	a.fundIncome = sumVouchers("type='fund_income' AND date >= ? AND date <= ?", from, to)
	a.refundReturn = sumVouchers("type='refund' AND refund_kind='return' AND date >= ? AND date <= ?", from, to)
	a.refundDestroy = sumVouchers("type='refund' AND refund_kind='destroy' AND date >= ? AND date <= ?", from, to)
	// 本期支出：按主凭证费用类别
	rows, err := db.Query(`
		SELECT COALESCE(NULLIF(v.expense_category,''),'other'), IFNULL(SUM(v.amount),0)
		FROM vouchers v
		WHERE v.status='normal' AND v.type='expense' AND v.date >= ? AND v.date <= ?
		GROUP BY COALESCE(NULLIF(v.expense_category,''),'other')`, from, to)
	if err == nil {
		for rows.Next() {
			var cat string
			var amt int64
			rows.Scan(&cat, &amt)
			switch cat {
			case "engineering":
				a.expEngineering += amt
			case "supervision":
				a.expSupervision += amt
			case "detection", "survey": // survey 为录入端"检测费、勘察设计费"的类别值
				a.expDetection += amt
			default:
				a.expOther += amt
			}
		}
		rows.Close()
	}
	return a
}

func (a monthAgg) expense() int64 {
	return a.expEngineering + a.expSupervision + a.expDetection + a.expOther + a.refundDestroy
}

// incomeNet 缴存净额 = 缴存 - 退返
func (a monthAgg) incomeNet() int64 {
	return a.income - a.refundReturn
}

// otherIncome 其他收入合计 = 经营/共用设施处置/其他收入
func (a monthAgg) otherIncome() int64 {
	return a.fundIncome
}

var expenseCatLabel = map[string]string{
	"engineering": "房屋维修工程（含电梯/消防/公共设施维修）",
	"supervision": "工程监理费",
	"detection":   "检测费",
	"other":       "其他支出",
}

// ==================== 月报表生成 ====================

// generateMonthlyReport 生成本月月报表 Excel（4 子表：汇总表/收入明细/支出明细/分楼栋结余表），
// 关键合计项均为活公式。返回生成的文件名。
func generateMonthlyReport(month string) (string, error) {
	if len(month) < 7 {
		return "", fmt.Errorf("月份格式错误")
	}
	mStart := month[:7] + "-01"
	mEnd := monthEnd(month)
	yStart := month[:4] + "-01-01"

	cm := collectAgg(mStart, mEnd)   // 本月
	cy := collectAgg(yStart, mEnd)   // 本年累计

	f := excelize.NewFile()
	const shSummary = "汇总表"
	const shIncome = "收入明细"
	const shExpense = "支出明细"
	const shBuilding = "分楼栋结余表"
	f.SetSheetName("Sheet1", shSummary)

	bold, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	set := func(sheet, cell string, v interface{}) { f.SetCellValue(sheet, cell, v) }

	// ---------- 汇总表 ----------
	set(shSummary, "A1", "住房维修基金月报表")
	f.SetCellStyle(shSummary, "A1", "A1", bold)
	set(shSummary, "A2", fmt.Sprintf("报告期：%s年%s月    生成时间：%s",
		month[:4], month[5:7], time.Now().Format("2006-01-02 15:04")))
	set(shSummary, "A4", "项目")
	set(shSummary, "B4", "本月")
	set(shSummary, "C4", "本年累计")
	f.SetCellStyle(shSummary, "A4", "C4", bold)

	set(shSummary, "A5", "期初结余")
	set(shSummary, "A6", "本期收入小计")
	set(shSummary, "A7", "  业主缴存")
	set(shSummary, "A8", "  减：退返交存")
	set(shSummary, "A9", "  利息收入")
	set(shSummary, "A10", "  其他收入")
	set(shSummary, "A11", "本期支出小计")
	set(shSummary, "A12", "  "+expenseCatLabel["engineering"])
	set(shSummary, "A13", "  "+expenseCatLabel["supervision"])
	set(shSummary, "A14", "  "+expenseCatLabel["detection"])
	set(shSummary, "A15", "  "+expenseCatLabel["other"])
	set(shSummary, "A16", "  灭失返还")
	set(shSummary, "A17", "期末结余")
	f.SetCellStyle(shSummary, "A17", "A17", bold)

	// 本月列
	set(shSummary, "B5", centsToYuan(cm.opening))
	set(shSummary, "B7", centsToYuan(cm.income))
	set(shSummary, "B8", centsToYuan(cm.refundReturn))
	set(shSummary, "B9", centsToYuan(cm.interest))
	set(shSummary, "B10", centsToYuan(cm.fundIncome))
	set(shSummary, "B12", centsToYuan(cm.expEngineering))
	set(shSummary, "B13", centsToYuan(cm.expSupervision))
	set(shSummary, "B14", centsToYuan(cm.expDetection))
	set(shSummary, "B15", centsToYuan(cm.expOther))
	set(shSummary, "B16", centsToYuan(cm.refundDestroy))
	// 本年累计列
	set(shSummary, "C5", centsToYuan(cy.opening))
	set(shSummary, "C7", centsToYuan(cy.income))
	set(shSummary, "C8", centsToYuan(cy.refundReturn))
	set(shSummary, "C9", centsToYuan(cy.interest))
	set(shSummary, "C10", centsToYuan(cy.fundIncome))
	set(shSummary, "C12", centsToYuan(cy.expEngineering))
	set(shSummary, "C13", centsToYuan(cy.expSupervision))
	set(shSummary, "C14", centsToYuan(cy.expDetection))
	set(shSummary, "C15", centsToYuan(cy.expOther))
	set(shSummary, "C16", centsToYuan(cy.refundDestroy))
	// 活公式：小计与期末（无条件写入，避免纯支出月份期末结余为空）
	for _, col := range []string{"B", "C"} {
		f.SetCellFormula(shSummary, col+"6", fmt.Sprintf("=%s7-%s8+%s9+%s10", col, col, col, col))
		f.SetCellFormula(shSummary, col+"11", fmt.Sprintf("=SUM(%s12:%s16)", col, col))
		f.SetCellFormula(shSummary, col+"17", fmt.Sprintf("=%s5+%s6-%s11", col, col, col))
	}
	f.SetCellStyle(shSummary, "B17", "C17", bold)
	set(shSummary, "A19", "口径说明：基金总额 = 户账（期初建账+缴存+利息分配-维修分摊-返还）+ 公共账（公共账期初+利息-已分配利息）；退返冲减缴存收入，灭失返还计入支出。")
	f.SetColWidth(shSummary, "A", "A", 42)
	f.SetColWidth(shSummary, "B", "C", 14)

	// ---------- 收入明细 / 支出明细 / 分楼栋结余表 ----------
	f.NewSheet(shIncome)
	f.NewSheet(shExpense)
	f.NewSheet(shBuilding)
	writeVoucherRows(f, shIncome, "income", mStart, mEnd)
	writeVoucherRows(f, shExpense, "expense", mStart, mEnd)
	writeBuildingSheet(f, shBuilding, mStart, mEnd)

	name := fmt.Sprintf("维修基金月报表-%s.xlsx", month[:7])
	path := filepath.Join(reportsDir, name)
	if err := f.SaveAs(path); err != nil {
		return "", err
	}
	return name, nil
}

// writeVoucherRows 写收入/支出明细子表，返回数据结束行号
func writeVoucherRows(f *excelize.File, sheet, vtype, mStart, mEnd string) int {
	bold, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	set := func(cell string, v interface{}) { f.SetCellValue(sheet, cell, v) }

	if vtype == "income" {
		set("A1", "收入明细（缴存 + 利息 + 其他收入 + 退返）")
		f.SetCellStyle(sheet, "A1", "A1", bold)
		for i, h := range []string{"日期", "凭证号", "小区", "楼洞", "户号/户主", "类型", "金额", "摘要"} {
			cell, _ := excelize.CoordinatesToCellName(i+1, 2)
			set(cell, h)
		}
		f.SetCellStyle(sheet, "A2", "H2", bold)
		rows, err := db.Query(`
			SELECT v.date, v.no, c.name, IFNULL(b.name,''),
				CASE WHEN h.id IS NULL THEN '' ELSE h.room_no || ' ' || h.owner END,
				v.type, v.amount, v.summary
			FROM vouchers v
			JOIN communities c ON v.community_id = c.id
			LEFT JOIN buildings b ON v.building_id = b.id
			LEFT JOIN households h ON v.household_id = h.id
			WHERE v.status='normal' AND (v.type IN ('income','interest','fund_income') OR (v.type='refund' AND v.refund_kind='return'))
			  AND v.date >= ? AND v.date <= ?
			ORDER BY v.date, v.no`, mStart, mEnd)
		if err != nil {
			return 2
		}
		defer rows.Close()
		r := 3
		for rows.Next() {
			var date, no, community, building, household, vt, summary string
			var amount int64
			rows.Scan(&date, &no, &community, &building, &household, &vt, &amount, &summary)
			typ := "缴存"
			if vt == "interest" {
				typ = "利息"
			} else if vt == "refund" {
				typ = "退返"
			} else if vt == "fund_income" {
				typ = "其他收入"
			}
			vals := []interface{}{date, no, community, building, household, typ, centsToYuan(amount), summary}
			for i, v := range vals {
				cell, _ := excelize.CoordinatesToCellName(i+1, r)
				set(cell, v)
			}
			r++
		}
		set(fmt.Sprintf("A%d", r), "合计")
		f.SetCellStyle(sheet, fmt.Sprintf("A%d", r), fmt.Sprintf("A%d", r), bold)
		f.SetCellFormula(sheet, fmt.Sprintf("G%d", r), fmt.Sprintf("=SUM(G3:G%d)", r-1))
		f.SetColWidth(sheet, "A", "A", 12)
		f.SetColWidth(sheet, "B", "B", 16)
		f.SetColWidth(sheet, "C", "E", 14)
		f.SetColWidth(sheet, "H", "H", 36)
		return r
	}

	// 支出明细（维修支出 + 灭失返还）
	set("A1", "支出明细（维修支出 + 灭失返还）")
	f.SetCellStyle(sheet, "A1", "A1", bold)
	for i, h := range []string{"日期", "凭证号", "小区", "楼洞", "费用类别", "金额", "维修项目/摘要", "经办"} {
		cell, _ := excelize.CoordinatesToCellName(i+1, 2)
		set(cell, h)
	}
	f.SetCellStyle(sheet, "A2", "H2", bold)
	rows, err := db.Query(`
		SELECT v.date, v.no, c.name, IFNULL(b.name,'全体楼洞'),
			COALESCE(NULLIF(v.expense_category,''),'other'), v.amount, v.summary,
			v.refund_kind
		FROM vouchers v
		JOIN communities c ON v.community_id = c.id
		LEFT JOIN buildings b ON v.building_id = b.id
		WHERE v.status='normal' AND (v.type='expense' OR (v.type='refund' AND v.refund_kind='destroy'))
		  AND v.date >= ? AND v.date <= ?
		ORDER BY v.date, v.no`, mStart, mEnd)
	if err != nil {
		return 2
	}
	defer rows.Close()
	r := 3
	for rows.Next() {
		var date, no, community, building, cat, summary, refundKind string
		var amount int64
		rows.Scan(&date, &no, &community, &building, &cat, &amount, &summary, &refundKind)
		label := "灭失返还"
		if refundKind != "destroy" {
			label = expenseCatLabel[cat]
			if label == "" {
				label = expenseCatLabel["other"]
			}
		}
		vals := []interface{}{date, no, community, building, label, centsToYuan(amount), summary, ""}
		for i, v := range vals {
			cell, _ := excelize.CoordinatesToCellName(i+1, r)
			set(cell, v)
		}
		r++
	}
	set(fmt.Sprintf("A%d", r), "合计")
	f.SetCellStyle(sheet, fmt.Sprintf("A%d", r), fmt.Sprintf("A%d", r), bold)
	f.SetCellFormula(sheet, fmt.Sprintf("F%d", r), fmt.Sprintf("=SUM(F3:F%d)", r-1))
	f.SetColWidth(sheet, "A", "A", 12)
	f.SetColWidth(sheet, "B", "B", 16)
	f.SetColWidth(sheet, "C", "D", 14)
	f.SetColWidth(sheet, "E", "E", 30)
	f.SetColWidth(sheet, "G", "G", 36)
	return r
}

// writeBuildingSheet 分楼栋结余表：期初 / 本月缴存 / 本月利息分配 / 本月返还退返 / 本月支出分摊 / 期末（公式）
func writeBuildingSheet(f *excelize.File, sheet, mStart, mEnd string) {
	bold, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	set := func(cell string, v interface{}) { f.SetCellValue(sheet, cell, v) }

	for i, h := range []string{"小区", "楼栋", "户数", "期初结余", "本月缴存", "本月利息分配", "本月返还/退返", "本月支出分摊", "期末结余"} {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		set(cell, h)
	}
	f.SetCellStyle(sheet, "A1", "I1", bold)

	type bRow struct {
		community, building             string
		hhCount                         int
		opening, inc, allocIn           int64
		refund, alloc, closing          int64
	}
	rows, err := db.Query(`
		SELECT c.name, b.name, b.id,
			(SELECT COUNT(*) FROM households h WHERE h.building_id = b.id)
		FROM buildings b JOIN communities c ON b.community_id = c.id
		ORDER BY c.name, b.name`)
	if err != nil {
		return
	}
	defer rows.Close()
	list := []bRow{}
	ids := []int64{}
	for rows.Next() {
		var br bRow
		var bid int64
		rows.Scan(&br.community, &br.building, &bid, &br.hhCount)
		list = append(list, br)
		ids = append(ids, bid)
	}
	rows.Close()

	deltaExpr := replaceAlias(householdDeltaExpr, "h")
	r := 2
	for i, br := range list {
		// 楼栋当前户账余额（与四级账同源：期初建账+缴存+利息分配-分摊-返还）
		var closing int64
		db.QueryRow(`SELECT IFNULL(SUM(h.opening_balance + `+deltaExpr+`),0)
			FROM households h WHERE h.building_id = ?`, ids[i]).Scan(&closing)
		// 本月缴存 / 利息分配 / 返还 / 分摊（按户所在楼栋归集）
		var inc, allocIn, refund, alloc int64
		db.QueryRow(`SELECT IFNULL(SUM(v.amount),0) FROM vouchers v
			JOIN households h ON v.household_id=h.id WHERE v.status='normal' AND v.type='income'
			AND v.date>=? AND v.date<=? AND h.building_id = ?`, mStart, mEnd, ids[i]).Scan(&inc)
		db.QueryRow(`SELECT IFNULL(SUM(v.amount),0) FROM vouchers v
			JOIN households h ON v.household_id=h.id WHERE v.status='normal' AND v.type='interest_alloc_child'
			AND v.date>=? AND v.date<=? AND h.building_id = ?`, mStart, mEnd, ids[i]).Scan(&allocIn)
		db.QueryRow(`SELECT IFNULL(SUM(v.amount),0) FROM vouchers v
			JOIN households h ON v.household_id=h.id WHERE v.status='normal' AND v.type='refund'
			AND v.date>=? AND v.date<=? AND h.building_id = ?`, mStart, mEnd, ids[i]).Scan(&refund)
		db.QueryRow(`SELECT IFNULL(SUM(v.amount),0) FROM vouchers v
			JOIN households h ON v.household_id=h.id WHERE v.status='normal' AND v.type='allocate'
			AND v.date>=? AND v.date<=? AND h.building_id = ?`, mStart, mEnd, ids[i]).Scan(&alloc)
		br.inc, br.allocIn, br.refund, br.alloc = inc, allocIn, refund, alloc
		br.closing = closing
		br.opening = br.closing - br.inc - br.allocIn + br.refund + br.alloc

		vals := []interface{}{br.community, br.building, br.hhCount, centsToYuan(br.opening),
			centsToYuan(br.inc), centsToYuan(br.allocIn), centsToYuan(br.refund), centsToYuan(br.alloc)}
		for i, v := range vals {
			cell, _ := excelize.CoordinatesToCellName(i+1, r)
			set(cell, v)
		}
		// 期末 = 期初 + 缴存 + 利息分配 - 返还退返 - 分摊（活公式）
		cell, _ := excelize.CoordinatesToCellName(9, r)
		f.SetCellFormula(sheet, cell, fmt.Sprintf("=D%d+E%d+F%d-G%d-H%d", r, r, r, r, r))
		r++
	}
	// 合计行（活公式）
	set(fmt.Sprintf("A%d", r), "合计")
	f.SetCellStyle(sheet, fmt.Sprintf("A%d", r), fmt.Sprintf("A%d", r), bold)
	f.SetCellFormula(sheet, fmt.Sprintf("C%d", r), fmt.Sprintf("=SUM(C2:C%d)", r-1))
	for _, col := range []string{"D", "E", "F", "G", "H", "I"} {
		f.SetCellFormula(sheet, fmt.Sprintf("%s%d", col, r), fmt.Sprintf("=SUM(%s2:%s%d)", col, col, r-1))
	}
	f.SetColWidth(sheet, "A", "B", 14)
	f.SetColWidth(sheet, "C", "I", 14)
}

// ==================== HTTP 处理器 ====================

// GET /api/reports/monthly  已生成的月报表列表
func listMonthlyReports(c *gin.Context) {
	entries, err := os.ReadDir(reportsDir)
	if err != nil {
		c.JSON(http.StatusOK, []gin.H{})
		return
	}
	out := []gin.H{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".xlsx") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, gin.H{
			"name": e.Name(),
			"size": info.Size(),
			"modified": info.ModTime().Format("2006-01-02 15:04"),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["name"].(string) > out[j]["name"].(string) })
	c.JSON(http.StatusOK, out)
}

// POST /api/reports/monthly  {month}  生成/重新生成
func regenerateMonthlyReport(c *gin.Context) {
	var req struct {
		Month string `json:"month" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Month) < 7 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请提供月份，如 2026-09"})
		return
	}
	name, err := generateMonthlyReport(req.Month)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成失败：" + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "name": name})
}

// GET /api/reports/monthly/file?name=  下载
func downloadMonthlyReport(c *gin.Context) {
	name := c.Query("name")
	if name == "" || filepath.Base(name) != name || !strings.HasSuffix(name, ".xlsx") || strings.Contains(name, "..") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法文件名"})
		return
	}
	path := filepath.Join(reportsDir, name)
	if _, err := os.Stat(path); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "文件不存在"})
		return
	}
	c.FileAttachment(path, name)
}
