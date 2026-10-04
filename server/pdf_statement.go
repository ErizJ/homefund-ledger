package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jung-kurt/gofpdf"
)

//go:embed assets/fonts/NotoSansSC.ttf
var notoFont []byte

// newStatementPDF 横向 A4、嵌入 Noto Sans SC 的报表 PDF 骨架
func newStatementPDF() *gofpdf.Fpdf {
	pdf := gofpdf.New("L", "mm", "A4", "")
	pdf.AddUTF8FontFromBytes("noto", "", notoFont)
	pdf.SetMargins(10, 10, 10)
	return pdf
}

func pdfMoney(v float64) string {
	return fmt.Sprintf("%.2f", v)
}

// pdfMeta 报表抬头（标题 + 会住维XX表 + 编制单位 + 期间 + 单位）
func pdfMeta(pdf *gofpdf.Fpdf, title, tableNo, period string) {
	pdf.SetFont("noto", "", 15)
	pdf.CellFormat(0, 9, title, "", 1, "C", false, 0, "")
	pdf.SetFont("noto", "", 9)
	pdf.CellFormat(0, 5.5, "资金名称：住宅专项维修资金　"+tableNo+"　单位：元", "", 1, "C", false, 0, "")
	pdf.CellFormat(0, 5.5, "编制单位："+orgName()+"　　期间："+period+"　　打印时间："+time.Now().Format("2006-01-02 15:04"), "", 1, "C", false, 0, "")
	pdf.Ln(3)
}

// pdfTable 渲染一张表格
func pdfTable(pdf *gofpdf.Fpdf, headers []string, widths []float64, rows [][]string, boldRows map[int]bool) {
	pdf.SetFont("noto", "", 9)
	// 表头
	pdf.SetFillColor(240, 242, 245)
	for i, h := range headers {
		align := "L"
		if i > 0 {
			align = "R"
		}
		pdf.CellFormat(widths[i], 7, h, "1", 0, align, true, 0, "")
	}
	pdf.Ln(-1)
	// 数据行
	for ri, row := range rows {
		for i, cell := range row {
			align := "L"
			if i > 0 {
				align = "R"
			}
			pdf.CellFormat(widths[i], 6.5, cell, "1", 0, align, false, 0, "")
		}
		pdf.Ln(-1)
		_ = boldRows
		_ = ri
	}
}

// bsDataRows 资产负债表 → PDF 行（资产段 + 负债净资产段）
func bsDataRows(month string) ([][]string, error) {
	year := month[:4]
	openingDate := strconv.Itoa(atoiYear(year)-1) + "-12-31"
	closingDate := monthEnd(month)
	assets, err := buildBSRows([]string{"asset"}, openingDate, closingDate)
	if err != nil {
		return nil, err
	}
	equity, err := buildBSRows([]string{"liability", "net_asset"}, openingDate, closingDate)
	if err != nil {
		return nil, err
	}
	rows := [][]string{{"资　产", "", "", "", "", "", ""}}
	for _, r := range assets {
		rows = append(rows, []string{r.Name,
			pdfMoney(centsToYuan(r.Open.comm)), pdfMoney(centsToYuan(r.Open.pub)), pdfMoney(centsToYuan(r.Open.total)),
			pdfMoney(centsToYuan(r.Clos.comm)), pdfMoney(centsToYuan(r.Clos.pub)), pdfMoney(centsToYuan(r.Clos.total))})
	}
	aO := totalsOfBS(assets, func(r bsRowOut) fundBalances { return r.Open })
	aC := totalsOfBS(assets, func(r bsRowOut) fundBalances { return r.Clos })
	rows = append(rows, []string{"资产总计",
		pdfMoney(centsToYuan(aO.comm)), pdfMoney(centsToYuan(aO.pub)), pdfMoney(centsToYuan(aO.total)),
		pdfMoney(centsToYuan(aC.comm)), pdfMoney(centsToYuan(aC.pub)), pdfMoney(centsToYuan(aC.total))})
	rows = append(rows, []string{"负债和净资产", "", "", "", "", "", ""})
	for _, r := range equity {
		rows = append(rows, []string{r.Name,
			pdfMoney(centsToYuan(r.Open.comm)), pdfMoney(centsToYuan(r.Open.pub)), pdfMoney(centsToYuan(r.Open.total)),
			pdfMoney(centsToYuan(r.Clos.comm)), pdfMoney(centsToYuan(r.Clos.pub)), pdfMoney(centsToYuan(r.Clos.total))})
	}
	eO := totalsOfBS(equity, func(r bsRowOut) fundBalances { return r.Open })
	eC := totalsOfBS(equity, func(r bsRowOut) fundBalances { return r.Clos })
	rows = append(rows, []string{"负债和净资产总计",
		pdfMoney(centsToYuan(eO.comm)), pdfMoney(centsToYuan(eO.pub)), pdfMoney(centsToYuan(eO.total)),
		pdfMoney(centsToYuan(eC.comm)), pdfMoney(centsToYuan(eC.pub)), pdfMoney(centsToYuan(eC.total))})
	return rows, nil
}

var pdfBSHeaders = []string{"项目", "商品住宅\n年初余额", "公有住房\n年初余额", "合计\n年初余额", "商品住宅\n期末余额", "公有住房\n期末余额", "合计\n期末余额"}
var pdfBSWidths = []float64{66, 30, 30, 30, 30, 30, 30}

// GET /api/gl/balance-sheet/pdf?month=
func glBalanceSheetPDF(c *gin.Context) {
	month := c.Query("month")
	if len(month) < 7 {
		month = time.Now().Format("2006-01")
	}
	rows, err := bsDataRows(month)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	pdf := newStatementPDF()
	pdf.AddPage()
	pdfMeta(pdf, "住宅专项维修资金资产负债表", "会住维01表", monthEnd(month))
	pdfTable(pdf, pdfBSHeaders, pdfBSWidths, rows, nil)
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="资产负债表-%s.pdf"`, month))
	c.Data(http.StatusOK, "application/pdf", buf.Bytes())
}

// isDataRows 收支表 → PDF 行
func isDataRows(month, year string) ([][]string, string, string, error) {
	annual := year != ""
	var from, to, cumFrom, cumTo string
	if annual {
		from, to = year+"-01-01", year+"-12-31"
		prev := strconv.Itoa(atoiYear(year) - 1)
		cumFrom, cumTo = prev+"-01-01", prev+"-12-31"
	} else {
		from, to = month+"-01", monthEnd(month)
		cumFrom, cumTo = month[:4]+"-01-01", monthEnd(month)
	}
	inc, err := buildISRows([]string{"income"}, from, to, cumFrom, cumTo, true)
	if err != nil {
		return nil, "", "", err
	}
	exp, err := buildISRows([]string{"expense"}, from, to, cumFrom, cumTo, false)
	if err != nil {
		return nil, "", "", err
	}
	rows := [][]string{{"一、本期收入", "", "", "", "", "", ""}}
	for _, r := range inc {
		rows = append(rows, []string{"　" + r.Name,
			pdfMoney(centsToYuan(r.Cur.comm)), pdfMoney(centsToYuan(r.Cur.pub)), pdfMoney(centsToYuan(r.Cur.total)),
			pdfMoney(centsToYuan(r.Cum.comm)), pdfMoney(centsToYuan(r.Cum.pub)), pdfMoney(centsToYuan(r.Cum.total))})
	}
	iC := totalsOfIS(inc, func(r isRowOut) fundBalances { return r.Cur })
	iM := totalsOfIS(inc, func(r isRowOut) fundBalances { return r.Cum })
	rows = append(rows, []string{"　收入合计",
		pdfMoney(centsToYuan(iC.comm)), pdfMoney(centsToYuan(iC.pub)), pdfMoney(centsToYuan(iC.total)),
		pdfMoney(centsToYuan(iM.comm)), pdfMoney(centsToYuan(iM.pub)), pdfMoney(centsToYuan(iM.total))})
	rows = append(rows, []string{"二、本期支出", "", "", "", "", "", ""})
	for _, r := range exp {
		rows = append(rows, []string{"　" + r.Name,
			pdfMoney(centsToYuan(r.Cur.comm)), pdfMoney(centsToYuan(r.Cur.pub)), pdfMoney(centsToYuan(r.Cur.total)),
			pdfMoney(centsToYuan(r.Cum.comm)), pdfMoney(centsToYuan(r.Cum.pub)), pdfMoney(centsToYuan(r.Cum.total))})
	}
	eC := totalsOfIS(exp, func(r isRowOut) fundBalances { return r.Cur })
	eM := totalsOfIS(exp, func(r isRowOut) fundBalances { return r.Cum })
	rows = append(rows, []string{"　支出合计",
		pdfMoney(centsToYuan(eC.comm)), pdfMoney(centsToYuan(eC.pub)), pdfMoney(centsToYuan(eC.total)),
		pdfMoney(centsToYuan(eM.comm)), pdfMoney(centsToYuan(eM.pub)), pdfMoney(centsToYuan(eM.total))})
	rows = append(rows, []string{"三、本期收支差额",
		pdfMoney(centsToYuan(iC.comm-eC.comm)), pdfMoney(centsToYuan(iC.pub-eC.pub)), pdfMoney(centsToYuan(iC.total-eC.total)),
		pdfMoney(centsToYuan(iM.comm-eM.comm)), pdfMoney(centsToYuan(iM.pub-eM.pub)), pdfMoney(centsToYuan(iM.total-eM.total))})
	curLabel, cumLabel := "本月数", "本年累计数"
	if annual {
		curLabel, cumLabel = "本年数", "上年数"
	}
	return rows, curLabel, cumLabel, nil
}

var pdfISWidths = []float64{56, 32, 32, 32, 32, 32, 32}

// GET /api/gl/income-statement/pdf?month= | ?year=
func glIncomeStatementPDF(c *gin.Context) {
	month, year := c.Query("month"), c.Query("year")
	if len(month) < 7 && len(year) != 4 {
		month = time.Now().Format("2006-01")
	}
	rows, curLabel, cumLabel, err := isDataRows(month, year)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	headers := []string{"项目", "商品住宅\n" + curLabel, "公有住房\n" + curLabel, "合计\n" + curLabel,
		"商品住宅\n" + cumLabel, "公有住房\n" + cumLabel, "合计\n" + cumLabel}
	period := month
	if year != "" {
		period = year + " 年度"
	}
	pdf := newStatementPDF()
	pdf.AddPage()
	pdfMeta(pdf, "住宅专项维修资金收支表", "会住维02表", period)
	pdfTable(pdf, headers, pdfISWidths, rows, nil)
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="收支表-%s.pdf"`, period))
	c.Data(http.StatusOK, "application/pdf", buf.Bytes())
}

// nasDataRows 净资产变动表 → PDF 行
func nasDataRows(month string) ([][]string, error) {
	_, open, diff, alloc, others, clos, err := buildNAS(month)
	if err != nil {
		return nil, err
	}
	f := func(v int64) string { return pdfMoney(centsToYuan(v)) }
	total := func(v []int64) string { return f(v[0] + v[1] + v[2]) }
	rows := [][]string{
		{"上年年末余额（本年年初余额）", f(open[0]), f(open[1]), f(open[2]), total(open)},
		{"本年变动：本年收支差额", f(diff[0]), f(diff[1]), f(diff[2]), total(diff)},
		{"本年变动：本年分配累计收益", f(alloc[0]), f(alloc[1]), f(alloc[2]), total(alloc)},
		{"本年变动：其他（划转/调整）", f(others[0]), f(others[1]), f(others[2]), total(others)},
		{"本年年末余额", f(clos[0]), f(clos[1]), f(clos[2]), total(clos)},
	}
	return rows, nil
}

// GET /api/gl/net-asset-statement/pdf?month=
func glNetAssetStatementPDF(c *gin.Context) {
	month := c.Query("month")
	if len(month) < 7 {
		month = time.Now().Format("2006-01")
	}
	rows, err := nasDataRows(month)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	pdf := newStatementPDF()
	pdf.AddPage()
	pdfMeta(pdf, "住宅专项维修资金净资产变动表", "会住维03表", month[:4]+" 年度")
	pdfTable(pdf, []string{"项目", "商品住宅维修资金", "已售公有住房维修资金", "待分配累计收益", "净资产合计"},
		[]float64{92, 40, 40, 40, 40}, rows, nil)
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="净资产变动表-%s.pdf"`, month[:4]))
	c.Data(http.StatusOK, "application/pdf", buf.Bytes())
}
