package main

import (
	"fmt"
	"path/filepath"
	"strconv"
	"time"

	"github.com/xuri/excelize/v2"
)

// generateStatementsSnapshot 生成月结时点的财务报表快照 Excel（会住维01/02/03表，分栏式）。
// 与实时查询同源同口径，但固化为文件存档：反结转改账后历史快照不变，与纸质归档一致。
func generateStatementsSnapshot(month string) (string, error) {
	year := month[:4]
	openingDate := strconv.Itoa(atoiYear(year)-1) + "-12-31"
	closingDate := monthEnd(month)

	f := excelize.NewFile()
	bold, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	set := func(sheet, cell string, v interface{}) { f.SetCellValue(sheet, cell, v) }
	head := func(sheet, title, tableNo string) int {
		set(sheet, "A1", title)
		f.SetCellStyle(sheet, "A1", "A1", bold)
		set(sheet, "A2", fmt.Sprintf("资金名称：住宅专项维修资金　%s　单位：元", tableNo))
		set(sheet, "A3", fmt.Sprintf("编制单位：%s　　期间：%s　　生成时间：%s", orgName(), month, time.Now().Format("2006-01-02 15:04")))
		return 5
	}

	// ---------- 会住维01表 资产负债表 ----------
	const shBS = "会住维01表"
	f.SetSheetName("Sheet1", shBS)
	r := head(shBS, "住宅专项维修资金资产负债表", "会住维01表")
	bsHeaders := []string{"项目", "商品住宅\n年初余额", "公有住房\n年初余额", "合计\n年初余额", "商品住宅\n期末余额", "公有住房\n期末余额", "合计\n期末余额"}
	for i, h := range bsHeaders {
		set(shBS, cellName(i+1, r), h)
	}
	f.SetCellStyle(shBS, cellName(1, r), cellName(7, r), bold)
	r++
	assets, err := buildBSRows([]string{"asset"}, openingDate, closingDate)
	if err != nil {
		return "", err
	}
	equity, err := buildBSRows([]string{"liability", "net_asset"}, openingDate, closingDate)
	if err != nil {
		return "", err
	}
	set(shBS, cellName(1, r), "资　产")
	r++
	bsRow := func(row int, name string, o, c fundBalances, isBold bool) {
		vals := []interface{}{name, centsToYuan(o.comm), centsToYuan(o.pub), centsToYuan(o.total),
			centsToYuan(c.comm), centsToYuan(c.pub), centsToYuan(c.total)}
		for i, v := range vals {
			set(shBS, cellName(i+1, row), v)
		}
		if isBold {
			f.SetCellStyle(shBS, cellName(1, row), cellName(7, row), bold)
		}
	}
	for _, a := range assets {
		bsRow(r, a.Name, a.Open, a.Clos, false)
		r++
	}
	aO := totalsOfBS(assets, func(x bsRowOut) fundBalances { return x.Open })
	aC := totalsOfBS(assets, func(x bsRowOut) fundBalances { return x.Clos })
	bsRow(r, "资产总计", aO, aC, true)
	r++
	set(shBS, cellName(1, r), "负债和净资产")
	r++
	for _, e := range equity {
		bsRow(r, e.Name, e.Open, e.Clos, false)
		r++
	}
	eO := totalsOfBS(equity, func(x bsRowOut) fundBalances { return x.Open })
	eC := totalsOfBS(equity, func(x bsRowOut) fundBalances { return x.Clos })
	bsRow(r, "负债和净资产总计", eO, eC, true)
	f.SetColWidth(shBS, "A", "A", 34)
	for _, col := range []string{"B", "C", "D", "E", "F", "G"} {
		f.SetColWidth(shBS, col, col, 14)
	}

	// ---------- 会住维02表 收支表 ----------
	const shIS = "会住维02表"
	f.NewSheet(shIS)
	r = head(shIS, "住宅专项维修资金收支表", "会住维02表")
	isHeaders := []string{"项目", "商品住宅\n本月数", "公有住房\n本月数", "合计\n本月数", "商品住宅\n本年累计数", "公有住房\n本年累计数", "合计\n本年累计数"}
	for i, h := range isHeaders {
		set(shIS, cellName(i+1, r), h)
	}
	f.SetCellStyle(shIS, cellName(1, r), cellName(7, r), bold)
	r++
	from, to := month+"-01", monthEnd(month)
	cumFrom, cumTo := year+"-01-01", monthEnd(month)
	inc, err := buildISRows([]string{"income"}, from, to, cumFrom, cumTo, true)
	if err != nil {
		return "", err
	}
	exp, err := buildISRows([]string{"expense"}, from, to, cumFrom, cumTo, false)
	if err != nil {
		return "", err
	}
	isRow := func(row int, name string, cur, cum fundBalances, isBold bool) {
		vals := []interface{}{name, centsToYuan(cur.comm), centsToYuan(cur.pub), centsToYuan(cur.total),
			centsToYuan(cum.comm), centsToYuan(cum.pub), centsToYuan(cum.total)}
		for i, v := range vals {
			set(shIS, cellName(i+1, row), v)
		}
		if isBold {
			f.SetCellStyle(shIS, cellName(1, row), cellName(7, row), bold)
		}
	}
	set(shIS, cellName(1, r), "一、本期收入")
	r++
	for _, x := range inc {
		isRow(r, "　"+x.Name, x.Cur, x.Cum, false)
		r++
	}
	iC := totalsOfIS(inc, func(x isRowOut) fundBalances { return x.Cur })
	iM := totalsOfIS(inc, func(x isRowOut) fundBalances { return x.Cum })
	isRow(r, "　收入合计", iC, iM, true)
	r++
	set(shIS, cellName(1, r), "二、本期支出")
	r++
	for _, x := range exp {
		isRow(r, "　"+x.Name, x.Cur, x.Cum, false)
		r++
	}
	eCC := totalsOfIS(exp, func(x isRowOut) fundBalances { return x.Cur })
	eMM := totalsOfIS(exp, func(x isRowOut) fundBalances { return x.Cum })
	isRow(r, "　支出合计", eCC, eMM, true)
	r++
	isRow(r, "三、本期收支差额",
		fundBalances{comm: iC.comm - eCC.comm, pub: iC.pub - eCC.pub, total: iC.total - eCC.total},
		fundBalances{comm: iM.comm - eMM.comm, pub: iM.pub - eMM.pub, total: iM.total - eMM.total}, true)
	f.SetColWidth(shIS, "A", "A", 34)
	for _, col := range []string{"B", "C", "D", "E", "F", "G"} {
		f.SetColWidth(shIS, col, col, 14)
	}

	// ---------- 会住维03表 净资产变动表 ----------
	const shNAS = "会住维03表"
	f.NewSheet(shNAS)
	r = head(shNAS, "住宅专项维修资金净资产变动表", "会住维03表")
	nasHeaders := []string{"项目", "商品住宅维修资金", "已售公有住房维修资金", "待分配累计收益", "净资产合计"}
	for i, h := range nasHeaders {
		set(shNAS, cellName(i+1, r), h)
	}
	f.SetCellStyle(shNAS, cellName(1, r), cellName(5, r), bold)
	r++
	_, open, diff, alloc, others, clos, err := buildNAS(month)
	if err != nil {
		return "", err
	}
	nasRow := func(row int, label string, vals []int64, isBold bool) {
		set(shNAS, cellName(1, row), label)
		for i, v := range vals {
			set(shNAS, cellName(i+2, row), centsToYuan(v))
		}
		if isBold {
			f.SetCellStyle(shNAS, cellName(1, row), cellName(5, row), bold)
		}
	}
	nasRow(r, "上年年末余额（本年年初余额）", open, false)
	r++
	nasRow(r, "本年变动：本年收支差额", diff, false)
	r++
	nasRow(r, "本年变动：本年分配累计收益", alloc, false)
	r++
	nasRow(r, "本年变动：其他（划转/调整）", others, false)
	r++
	nasRow(r, "本年年末余额", clos, true)
	f.SetColWidth(shNAS, "A", "A", 42)
	for _, col := range []string{"B", "C", "D", "E"} {
		f.SetColWidth(shNAS, col, col, 20)
	}

	name := fmt.Sprintf("财务报表-%s.xlsx", month)
	path := filepath.Join(reportsDir, name)
	if err := f.SaveAs(path); err != nil {
		return "", err
	}
	return name, nil
}

func cellName(col, row int) string {
	c, _ := excelize.CoordinatesToCellName(col, row)
	return c
}

// generateAnnualStatementsSnapshot 年结时生成年度财务报表快照（会住维01/02/03表，年度口径）
func generateAnnualStatementsSnapshot(year string) (string, error) {
	f := excelize.NewFile()
	bold, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	set := func(sheet, cell string, v interface{}) { f.SetCellValue(sheet, cell, v) }
	head := func(sheet, title, tableNo, period string) int {
		set(sheet, "A1", title)
		f.SetCellStyle(sheet, "A1", "A1", bold)
		set(sheet, "A2", fmt.Sprintf("资金名称：住宅专项维修资金　%s　单位：元", tableNo))
		set(sheet, "A3", fmt.Sprintf("编制单位：%s　　期间：%s　　生成时间：%s", orgName(), period, time.Now().Format("2006-01-02 15:04")))
		return 5
	}
	prev := strconv.Itoa(atoiYear(year) - 1)
	openingDate := prev + "-12-31"
	closingDate := year + "-12-31"

	// 会住维01表（年末）
	const shBS = "会住维01表"
	f.SetSheetName("Sheet1", shBS)
	r := head(shBS, "住宅专项维修资金资产负债表", "会住维01表", year+" 年度")
	bsHeaders := []string{"项目", "商品住宅\n年初余额", "公有住房\n年初余额", "合计\n年初余额", "商品住宅\n年末余额", "公有住房\n年末余额", "合计\n年末余额"}
	for i, h := range bsHeaders {
		set(shBS, cellName(i+1, r), h)
	}
	f.SetCellStyle(shBS, cellName(1, r), cellName(7, r), bold)
	r++
	assets, err := buildBSRows([]string{"asset"}, openingDate, closingDate)
	if err != nil {
		return "", err
	}
	equity, err := buildBSRows([]string{"liability", "net_asset"}, openingDate, closingDate)
	if err != nil {
		return "", err
	}
	set(shBS, cellName(1, r), "资　产")
	r++
	bsRow := func(row int, name string, o, c fundBalances, isBold bool) {
		vals := []interface{}{name, centsToYuan(o.comm), centsToYuan(o.pub), centsToYuan(o.total),
			centsToYuan(c.comm), centsToYuan(c.pub), centsToYuan(c.total)}
		for i, v := range vals {
			set(shBS, cellName(i+1, row), v)
		}
		if isBold {
			f.SetCellStyle(shBS, cellName(1, row), cellName(7, row), bold)
		}
	}
	for _, a := range assets {
		bsRow(r, a.Name, a.Open, a.Clos, false)
		r++
	}
	aO := totalsOfBS(assets, func(x bsRowOut) fundBalances { return x.Open })
	aC := totalsOfBS(assets, func(x bsRowOut) fundBalances { return x.Clos })
	bsRow(r, "资产总计", aO, aC, true)
	r++
	set(shBS, cellName(1, r), "负债和净资产")
	r++
	for _, e := range equity {
		bsRow(r, e.Name, e.Open, e.Clos, false)
		r++
	}
	eO := totalsOfBS(equity, func(x bsRowOut) fundBalances { return x.Open })
	eC := totalsOfBS(equity, func(x bsRowOut) fundBalances { return x.Clos })
	bsRow(r, "负债和净资产总计", eO, eC, true)
	f.SetColWidth(shBS, "A", "A", 34)
	for _, col := range []string{"B", "C", "D", "E", "F", "G"} {
		f.SetColWidth(shBS, col, col, 14)
	}

	// 会住维02表（本年数/上年数）
	const shIS = "会住维02表"
	f.NewSheet(shIS)
	r = head(shIS, "住宅专项维修资金收支表", "会住维02表", year+" 年度")
	isHeaders := []string{"项目", "商品住宅\n本年数", "公有住房\n本年数", "合计\n本年数", "商品住宅\n上年数", "公有住房\n上年数", "合计\n上年数"}
	for i, h := range isHeaders {
		set(shIS, cellName(i+1, r), h)
	}
	f.SetCellStyle(shIS, cellName(1, r), cellName(7, r), bold)
	r++
	from, to := year+"-01-01", year+"-12-31"
	cumFrom, cumTo := prev+"-01-01", prev+"-12-31"
	inc, err := buildISRows([]string{"income"}, from, to, cumFrom, cumTo, true)
	if err != nil {
		return "", err
	}
	exp, err := buildISRows([]string{"expense"}, from, to, cumFrom, cumTo, false)
	if err != nil {
		return "", err
	}
	isRow := func(row int, name string, cur, cum fundBalances, isBold bool) {
		vals := []interface{}{name, centsToYuan(cur.comm), centsToYuan(cur.pub), centsToYuan(cur.total),
			centsToYuan(cum.comm), centsToYuan(cum.pub), centsToYuan(cum.total)}
		for i, v := range vals {
			set(shIS, cellName(i+1, row), v)
		}
		if isBold {
			f.SetCellStyle(shIS, cellName(1, row), cellName(7, row), bold)
		}
	}
	set(shIS, cellName(1, r), "一、本期收入")
	r++
	for _, x := range inc {
		isRow(r, "　"+x.Name, x.Cur, x.Cum, false)
		r++
	}
	iC := totalsOfIS(inc, func(x isRowOut) fundBalances { return x.Cur })
	iM := totalsOfIS(inc, func(x isRowOut) fundBalances { return x.Cum })
	isRow(r, "　收入合计", iC, iM, true)
	r++
	set(shIS, cellName(1, r), "二、本期支出")
	r++
	for _, x := range exp {
		isRow(r, "　"+x.Name, x.Cur, x.Cum, false)
		r++
	}
	eCC := totalsOfIS(exp, func(x isRowOut) fundBalances { return x.Cur })
	eMM := totalsOfIS(exp, func(x isRowOut) fundBalances { return x.Cum })
	isRow(r, "　支出合计", eCC, eMM, true)
	r++
	isRow(r, "三、本期收支差额",
		fundBalances{comm: iC.comm - eCC.comm, pub: iC.pub - eCC.pub, total: iC.total - eCC.total},
		fundBalances{comm: iM.comm - eMM.comm, pub: iM.pub - eMM.pub, total: iM.total - eMM.total}, true)
	f.SetColWidth(shIS, "A", "A", 34)
	for _, col := range []string{"B", "C", "D", "E", "F", "G"} {
		f.SetColWidth(shIS, col, col, 14)
	}

	// 会住维03表
	const shNAS = "会住维03表"
	f.NewSheet(shNAS)
	r = head(shNAS, "住宅专项维修资金净资产变动表", "会住维03表", year+" 年度")
	nasHeaders := []string{"项目", "商品住宅维修资金", "已售公有住房维修资金", "待分配累计收益", "净资产合计"}
	for i, h := range nasHeaders {
		set(shNAS, cellName(i+1, r), h)
	}
	f.SetCellStyle(shNAS, cellName(1, r), cellName(5, r), bold)
	r++
	_, open, diff, alloc, others, clos, err := buildNAS(year + "-12")
	if err != nil {
		return "", err
	}
	nasRow := func(row int, label string, vals []int64, isBold bool) {
		set(shNAS, cellName(1, row), label)
		for i, v := range vals {
			set(shNAS, cellName(i+2, row), centsToYuan(v))
		}
		if isBold {
			f.SetCellStyle(shNAS, cellName(1, row), cellName(5, row), bold)
		}
	}
	nasRow(r, "上年年末余额（本年年初余额）", open, false)
	r++
	nasRow(r, "本年变动：本年收支差额", diff, false)
	r++
	nasRow(r, "本年变动：本年分配累计收益", alloc, false)
	r++
	nasRow(r, "本年变动：其他（划转/调整）", others, false)
	r++
	nasRow(r, "本年年末余额", clos, true)
	f.SetColWidth(shNAS, "A", "A", 42)
	for _, col := range []string{"B", "C", "D", "E"} {
		f.SetColWidth(shNAS, col, col, 20)
	}

	name := fmt.Sprintf("财务报表-%s年度.xlsx", year)
	path := filepath.Join(reportsDir, name)
	if err := f.SaveAs(path); err != nil {
		return "", err
	}
	return name, nil
}
