package app

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// ==================== 财务报表（财会〔2020〕7号附录 会住维01/02/03表） ====================
// 分栏式：按资金性质（商品住宅 / 已售公有住房）分栏，另设合计栏。
// 科目行由 gl_subjects 动态生成：一级科目为行，子科目按 parent+"01"（商品）/ parent+"02"（公房）归栏，
// 其余子科目（如国债专户）仅计入合计。支持自定义科目。

// subjInfo 科目信息
type subjInfo struct {
	code, name, typ, parent string
}

func listSubjects(types ...string) ([]subjInfo, error) {
	rows, err := db.Query(`SELECT code, name, type, parent FROM gl_subjects WHERE enabled=1 ORDER BY code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []subjInfo{}
	for rows.Next() {
		var s subjInfo
		rows.Scan(&s.code, &s.name, &s.typ, &s.parent)
		if len(types) > 0 {
			hit := false
			for _, t := range types {
				if s.typ == t {
					hit = true
					break
				}
			}
			if !hit {
				continue
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// fundCols 一级科目 → 分栏子科目：子科目 parent+"01" 归商品、parent+"02" 归公房、其余（如国债专户）仅计合计；
// 一级科目自身尾号 01/02 的（如 3001 商品住宅维修资金 / 3002 已售公有住房维修资金）整体归属对应栏。
func fundCols(top subjInfo, all []subjInfo) (comm, pub, others []string, total []string) {
	total = append(total, top.code) // 一级科目自身（手工凭证可能直接记一级）
	for _, s := range all {
		if s.parent == top.code {
			total = append(total, s.code)
			switch {
			case s.code == top.code+"01":
				comm = append(comm, s.code)
			case s.code == top.code+"02":
				pub = append(pub, s.code)
			default:
				others = append(others, s.code)
			}
		}
	}
	// 净资产类且无子科目的（3001 商品住宅维修资金 / 3002 已售公有住房维修资金）按一级科目自身归属分栏
	if len(comm) == 0 && len(pub) == 0 && top.typ == "net_asset" {
		if strings.HasSuffix(top.code, "01") {
			comm = append([]string{top.code}, comm...)
		} else if strings.HasSuffix(top.code, "02") {
			pub = append([]string{top.code}, pub...)
		}
	}
	return
}

func stringsToIface(s []string) []interface{} {
	out := make([]interface{}, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

func atoiYear(y string) int {
	n, _ := strconv.Atoi(y)
	return n
}

// glSetBalance 科目集合截至某日（含当日）的借余口径余额（借正贷负，单位分）。
// 期初建账凭证（kind='opening'）代表建账时点余额，无论其日期均计入（保证年初/期末口径一致）。
func glSetBalance(codes []string, asOf string) (int64, error) {
	if len(codes) == 0 {
		return 0, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(codes)), ",")
	var v int64
	args := append([]interface{}{asOf}, stringsToIface(codes)...)
	err := db.QueryRow(`SELECT IFNULL(SUM(CASE e.direction WHEN 'debit' THEN e.amount ELSE -e.amount END),0)
		FROM gl_entries e
		JOIN gl_vouchers v ON e.voucher_id=v.id AND v.status='normal' AND (v.date <= ? OR v.kind='opening')
		WHERE e.subject_code IN (`+placeholders+`)`, args...).Scan(&v)
	return v, err
}

// fundBalances 一个科目集合行的三栏余额（商品/公房/合计，借余口径，单位分）
type fundBalances struct {
	comm, pub, total int64
}

func balanceFor(codes commPubOthers, asOf string, creditSide bool) (fundBalances, error) {
	var b fundBalances
	var err error
	if b.comm, err = glSetBalance(codes.comm, asOf); err != nil {
		return b, err
	}
	if b.pub, err = glSetBalance(codes.pub, asOf); err != nil {
		return b, err
	}
	if b.total, err = glSetBalance(codes.total, asOf); err != nil {
		return b, err
	}
	if creditSide { // 负债/净资产为贷余科目，取反
		b.comm, b.pub, b.total = -b.comm, -b.pub, -b.total
	}
	return b, nil
}

type commPubOthers struct{ comm, pub, others, total []string }

// bsRowOut 资产负债表行
type bsRowOut struct {
	Code string
	Name string
	Open fundBalances
	Clos fundBalances
}

// buildBSRows 动态构造资产负债表行（借方类=资产，贷方类=负债/净资产）
func buildBSRows(types []string, openingDate, closingDate string) ([]bsRowOut, error) {
	all, err := listSubjects()
	if err != nil {
		return nil, err
	}
	tops, err := listSubjects(types...)
	if err != nil {
		return nil, err
	}
	out := []bsRowOut{}
	for _, t := range tops {
		if t.parent != "" {
			continue // 只取一级科目
		}
		comm, pub, others, total := fundCols(t, all)
		cols := commPubOthers{comm: comm, pub: pub, others: others, total: total}
		creditSide := t.typ != "asset"
		o, err := balanceFor(cols, openingDate, creditSide)
		if err != nil {
			return nil, err
		}
		cl, err := balanceFor(cols, closingDate, creditSide)
		if err != nil {
			return nil, err
		}
		out = append(out, bsRowOut{Code: t.code, Name: t.name, Open: o, Clos: cl})
	}
	return out, nil
}

// totalsOfBS 行合计（三栏）
func totalsOfBS(rows []bsRowOut, which func(bsRowOut) fundBalances) fundBalances {
	var t fundBalances
	for _, r := range rows {
		b := which(r)
		t.comm += b.comm
		t.pub += b.pub
		t.total += b.total
	}
	return t
}

// diagnostics 报表诊断引导（期初建账缺失 / 未月结 / 未历史补账）
type diagItem struct {
	Type    string `json:"type"`
	Level   string `json:"level"` // error | warn | info
	Message string `json:"message"`
}

func statementDiagnostics(month string) []diagItem {
	diags := []diagItem{}
	end := monthEnd(month)
	// ① 期初建账缺失（仅对有期初余额的小区：户账期初 > 0 或公共账期初 > 0）
	var missing []string
	rows, err := db.Query(`SELECT c.name FROM communities c
		WHERE (c.public_opening > 0 OR EXISTS (SELECT 1 FROM households h JOIN buildings b ON h.building_id=b.id
			WHERE b.community_id=c.id AND h.opening_balance > 0))
		  AND NOT EXISTS (SELECT 1 FROM gl_vouchers gv WHERE gv.source_type='opening' AND gv.source_id=c.id AND gv.status='normal')
		ORDER BY c.name`)
	if err == nil {
		for rows.Next() {
			var n string
			rows.Scan(&n)
			missing = append(missing, n)
		}
		rows.Close()
	}
	if len(missing) > 0 {
		diags = append(diags, diagItem{Type: "opening", Level: "error",
			Message: "以下小区未生成期初建账凭证：" + strings.Join(missing, "、") + "。请到「试算与对账」页点「生成期初凭证」，否则资产负债表的银行存款与净资产均缺失。"})
	}
	// ② 未结转（收入支出未转入净资产）
	var closedCnt int
	db.QueryRow(`SELECT COUNT(*) FROM periods WHERE month=?`, month).Scan(&closedCnt)
	if closedCnt == 0 {
		diags = append(diags, diagItem{Type: "unclosed", Level: "warn",
			Message: month + " 尚未结转：本月收入/支出尚未转入净资产，资产负债表中「资产」将大于「负债和净资产」，差额 = 未结转收支净额（执行结转后归零）。"})
	}
	// ③ 业务凭证所在的汇总记账凭证未生成（未历史补账）；挂主凭证的从属凭证不单独生成
	var backfillCnt int
	db.QueryRow(`SELECT COUNT(*) FROM vouchers v
		WHERE v.status='normal' AND v.master_id IS NULL
		  AND v.type IN ('income','interest','expense','refund','fund_income','cash','bond','interest_alloc')
		  AND v.date <= ?
		  AND NOT EXISTS (SELECT 1 FROM gl_vouchers gv
			WHERE gv.source_type='agg' AND gv.source_id=v.community_id AND gv.date=v.date AND gv.status='normal')`,
		end).Scan(&backfillCnt)
	if backfillCnt > 0 {
		if !shouldAutoGL() {
			diags = append(diags, diagItem{Type: "backfill", Level: "info",
				Message: "已关闭「日常录入自动生成记账凭证」：财务账套由手工凭证维护，业务台账与财务账之间将存在差额属正常。如需恢复自动生成，请在基础数据开启开关并点「历史补账」。"})
		} else {
			diags = append(diags, diagItem{Type: "backfill", Level: "error",
				Message: "有 " + strconv.Itoa(backfillCnt) + " 张业务凭证所在的汇总记账凭证尚未生成（启用财务账套前的历史凭证）。请到「试算与对账」页点「历史补账」。补账后需重新结转。"})
		}
	}
	if len(diags) == 0 {
		diags = append(diags, diagItem{Type: "ok", Level: "info", Message: "未发现异常：期初建账、结转、历史补账均已到位。"})
	}
	return diags
}

func bsJSON(rows []bsRowOut) []gin.H {
	out := []gin.H{}
	for _, r := range rows {
		out = append(out, gin.H{
			"code": r.Code, "name": r.Name,
			"openComm": centsToYuan(r.Open.comm), "openPub": centsToYuan(r.Open.pub), "openTotal": centsToYuan(r.Open.total),
			"closComm": centsToYuan(r.Clos.comm), "closPub": centsToYuan(r.Clos.pub), "closTotal": centsToYuan(r.Clos.total),
		})
	}
	return out
}

func fbJSON(b fundBalances) gin.H {
	return gin.H{"comm": centsToYuan(b.comm), "pub": centsToYuan(b.pub), "total": centsToYuan(b.total)}
}

// GET /api/gl/balance-sheet?month=2026-09  会住维01表（分栏式）
func glBalanceSheet(c *gin.Context) {
	month := c.Query("month")
	if len(month) < 7 {
		month = time.Now().Format("2006-01")
	}
	if _, err := time.Parse("2006-01", month); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "月份格式应为 yyyy-MM"})
		return
	}
	year := month[:4]
	openingDate := strconv.Itoa(atoiYear(year)-1) + "-12-31" // 年初余额 = 上年年末
	closingDate := monthEnd(month)

	assets, err := buildBSRows([]string{"asset"}, openingDate, closingDate)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	equity, err := buildBSRows([]string{"liability", "net_asset"}, openingDate, closingDate)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	aOpen, aClose := totalsOfBS(assets, func(r bsRowOut) fundBalances { return r.Open }),
		totalsOfBS(assets, func(r bsRowOut) fundBalances { return r.Clos })
	eOpen, eClose := totalsOfBS(equity, func(r bsRowOut) fundBalances { return r.Open }),
		totalsOfBS(equity, func(r bsRowOut) fundBalances { return r.Clos })

	c.JSON(http.StatusOK, gin.H{
		"year": year, "month": month, "asOf": closingDate, "org": orgName(),
		"assets": bsJSON(assets), "equity": bsJSON(equity),
		"assetsTotalOpening": fbJSON(aOpen), "assetsTotalClosing": fbJSON(aClose),
		"equityTotalOpening": fbJSON(eOpen), "equityTotalClosing": fbJSON(eClose),
		"balanced":    aClose.total == eClose.total,
		"diagnostics": statementDiagnostics(month),
	})
}

// ==================== 会住维02表 收支表（分栏式） ====================

// glPeriodNet 科目集合在 [from, to] 内、剔除结转/期初建账凭证后的净发生额（贷正借负，单位分）
func glPeriodNet(codes []string, from, to string) (int64, error) {
	if len(codes) == 0 {
		return 0, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(codes)), ",")
	var v int64
	args := append([]interface{}{from, to}, stringsToIface(codes)...)
	err := db.QueryRow(`SELECT IFNULL(SUM(CASE e.direction WHEN 'credit' THEN e.amount ELSE -e.amount END),0)
		FROM gl_entries e
		JOIN gl_vouchers v ON e.voucher_id=v.id AND v.status='normal' AND v.kind NOT IN ('closing','opening')
			AND v.date >= ? AND v.date <= ?
		WHERE e.subject_code IN (`+placeholders+`)`, args...).Scan(&v)
	return v, err
}

// netFor 一个科目集合行在两个期间的净额（收入=贷净，支出=借净）
func netFor(codes commPubOthers, from, to, cumFrom, cumTo string, isIncome bool) (cur, cum fundBalances, err error) {
	toBal := func(v int64) int64 {
		if !isIncome {
			return -v
		}
		return v
	}
	if cur.comm, err = glPeriodNet(codes.comm, from, to); err != nil {
		return
	}
	cur.comm = toBal(cur.comm)
	if cur.pub, err = glPeriodNet(codes.pub, from, to); err != nil {
		return
	}
	cur.pub = toBal(cur.pub)
	if cur.total, err = glPeriodNet(codes.total, from, to); err != nil {
		return
	}
	cur.total = toBal(cur.total)
	if cum.comm, err = glPeriodNet(codes.comm, cumFrom, cumTo); err != nil {
		return
	}
	cum.comm = toBal(cum.comm)
	if cum.pub, err = glPeriodNet(codes.pub, cumFrom, cumTo); err != nil {
		return
	}
	cum.pub = toBal(cum.pub)
	if cum.total, err = glPeriodNet(codes.total, cumFrom, cumTo); err != nil {
		return
	}
	cum.total = toBal(cum.total)
	return
}

type isRowOut struct {
	Code string
	Name string
	Cur  fundBalances
	Cum  fundBalances
}

func buildISRows(types []string, from, to, cumFrom, cumTo string, isIncome bool) ([]isRowOut, error) {
	all, err := listSubjects()
	if err != nil {
		return nil, err
	}
	tops, err := listSubjects(types...)
	if err != nil {
		return nil, err
	}
	out := []isRowOut{}
	for _, t := range tops {
		if t.parent != "" {
			continue
		}
		comm, pub, others, total := fundCols(t, all)
		cols := commPubOthers{comm: comm, pub: pub, others: others, total: total}
		cur, cum, err := netFor(cols, from, to, cumFrom, cumTo, isIncome)
		if err != nil {
			return nil, err
		}
		out = append(out, isRowOut{Code: t.code, Name: t.name, Cur: cur, Cum: cum})
	}
	return out, nil
}

func isJSON(rows []isRowOut) []gin.H {
	out := []gin.H{}
	for _, r := range rows {
		out = append(out, gin.H{
			"code": r.Code, "name": r.Name,
			"curComm": centsToYuan(r.Cur.comm), "curPub": centsToYuan(r.Cur.pub), "curTotal": centsToYuan(r.Cur.total),
			"cumComm": centsToYuan(r.Cum.comm), "cumPub": centsToYuan(r.Cum.pub), "cumTotal": centsToYuan(r.Cum.total),
		})
	}
	return out
}

func totalsOfIS(rows []isRowOut, which func(isRowOut) fundBalances) fundBalances {
	var t fundBalances
	for _, r := range rows {
		b := which(r)
		t.comm += b.comm
		t.pub += b.pub
		t.total += b.total
	}
	return t
}

// GET /api/gl/income-statement?month=2026-09 | ?year=2026  会住维02表
func glIncomeStatement(c *gin.Context) {
	month, year := c.Query("month"), c.Query("year")
	annual := false
	var from, to, cumFrom, cumTo, title string
	switch {
	case year != "" && len(year) == 4:
		annual = true
		from, to = year+"-01-01", year+"-12-31"
		prev := strconv.Itoa(atoiYear(year) - 1)
		cumFrom, cumTo = prev+"-01-01", prev+"-12-31"
		title = year + " 年度"
	case month != "" && len(month) == 7:
		from, to = month+"-01", monthEnd(month)
		cumFrom, cumTo = month[:4]+"-01-01", monthEnd(month)
		title = month
	default:
		m := time.Now().Format("2006-01")
		from, to = m+"-01", monthEnd(m)
		cumFrom, cumTo = m[:4]+"-01-01", monthEnd(m)
		title = m
	}

	inc, err := buildISRows([]string{"income"}, from, to, cumFrom, cumTo, true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	exp, err := buildISRows([]string{"expense"}, from, to, cumFrom, cumTo, false)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	incCur, incCum := totalsOfIS(inc, func(r isRowOut) fundBalances { return r.Cur }),
		totalsOfIS(inc, func(r isRowOut) fundBalances { return r.Cum })
	expCur, expCum := totalsOfIS(exp, func(r isRowOut) fundBalances { return r.Cur }),
		totalsOfIS(exp, func(r isRowOut) fundBalances { return r.Cum })
	diffCur := fundBalances{comm: incCur.comm - expCur.comm, pub: incCur.pub - expCur.pub, total: incCur.total - expCur.total}
	diffCum := fundBalances{comm: incCum.comm - expCum.comm, pub: incCum.pub - expCum.pub, total: incCum.total - expCum.total}

	curLabel, cumLabel := "本月数", "本年累计数"
	if annual {
		curLabel, cumLabel = "本年数", "上年数"
	}
	c.JSON(http.StatusOK, gin.H{
		"mode":  map[bool]string{true: "year", false: "month"}[annual],
		"month": month, "year": year, "title": title, "org": orgName(),
		"curLabel": curLabel, "cumLabel": cumLabel,
		"income": isJSON(inc), "expense": isJSON(exp),
		"incomeTotal": fbJSON(incCur), "expenseTotal": fbJSON(expCur),
		"incomeCumTotal": fbJSON(incCum), "expenseCumTotal": fbJSON(expCum),
		"diff": fbJSON(diffCur), "diffCumulative": fbJSON(diffCum),
	})
}

// ==================== 会住维03表 净资产变动表 ====================

type nasRowOut struct {
	Label string
	Cols  []float64 // [商品住宅维修资金, 已售公有住房维修资金, 待分配累计收益, 净资产合计]
}

// credBalance 科目集合截至某日的贷余口径余额（贷正借负，单位分）
func credBalance(codes []string, asOf string) (int64, error) {
	v, err := glSetBalance(codes, asOf)
	return -v, err
}

// glPeriodNetByFund 科目集合在 [from, to] 内按项目资金性质分组的净发生额（贷正借负，单位分）。
// 用于无分栏子科目的支出类（5001xx 为商品/公房共用，靠项目归属）。
func glPeriodNetByFund(codes []string, from, to string) (comm, pub int64, err error) {
	placeholders := strings.TrimRight(strings.Repeat("?,", len(codes)), ",")
	args := append([]interface{}{from, to}, stringsToIface(codes)...)
	rows, err := db.Query(`SELECT IFNULL(c.fund_type,'commercial'),
			IFNULL(SUM(CASE e.direction WHEN 'credit' THEN e.amount ELSE -e.amount END),0)
		FROM gl_entries e
		JOIN gl_vouchers v ON e.voucher_id=v.id AND v.status='normal' AND v.kind != 'closing'
			AND v.date >= ? AND v.date <= ?
		LEFT JOIN communities c ON e.project_id=c.id
		WHERE e.subject_code IN (`+placeholders+`)
		GROUP BY IFNULL(c.fund_type,'commercial')`, args...)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var ft string
		var v int64
		rows.Scan(&ft, &v)
		if ft == "public" {
			pub += v
		} else {
			comm += v
		}
	}
	return comm, pub, nil
}

// buildNAS 会住维03表数据：返回 年初/收支差额/分配/其他变动/年末（三栏：3001/3002/3101，贷正借负，单位分）
func buildNAS(month string) (year string, open, diff, alloc, others, clos []int64, err error) {
	year = month[:4]
	prev := strconv.Itoa(atoiYear(year) - 1)
	openingDate := prev + "-12-31"
	from, to := year+"-01-01", year+"-12-31"

	open = make([]int64, 3)
	clos = make([]int64, 3)
	for i, codes := range [][]string{{"3001"}, {"3002"}, {"3101", "310101", "310102"}} {
		if open[i], err = credBalance(codes, openingDate); err != nil {
			return
		}
		if clos[i], err = credBalance(codes, to); err != nil {
			return
		}
	}
	diff = make([]int64, 3)
	contribComm, e := glPeriodNet([]string{"4001", "400101"}, from, to)
	if e != nil {
		err = e
		return
	}
	contribPub, e := glPeriodNet([]string{"400102"}, from, to)
	if e != nil {
		err = e
		return
	}
	expComm, expPub, e := glPeriodNetByFund([]string{"5001", "500101", "500102", "500103", "500104", "5101", "510101", "510102"}, from, to)
	if e != nil {
		err = e
		return
	}
	diff[0] = contribComm + expComm // expComm 为贷正口径（支出为负），直接相加
	diff[1] = contribPub + expPub
	gainComm, e := glPeriodNet([]string{"4101", "410101", "4102", "410201", "4201", "420101", "4301", "430101", "4901", "490101"}, from, to)
	if e != nil {
		err = e
		return
	}
	gainPub, e := glPeriodNet([]string{"410102", "410202", "420102", "430102", "490102"}, from, to)
	if e != nil {
		err = e
		return
	}
	diff[2] = gainComm + gainPub
	alloc = make([]int64, 3)
	for i, codes := range [][]string{{"3001"}, {"3002"}, {"3101", "310101", "310102"}} {
		if alloc[i], err = glPeriodNet(codes, from, to); err != nil {
			return
		}
	}
	others = make([]int64, 3)
	for i := 0; i < 3; i++ {
		others[i] = clos[i] - open[i] - diff[i] - alloc[i]
	}
	return
}

// GET /api/gl/net-asset-statement?month=2026-09  会住维03表 净资产变动表（年度口径，取所选月所在年度）
// 口径（贷正借负）：年末 = 年初 + 本年收支差额 + 本年分配累计收益 + 其他变动；
// "其他变动" = 实际余额反推的差额（手工凭证直接记 3 类科目、划转等），通常为 0，非 0 时可自查。
func glNetAssetStatement(c *gin.Context) {
	month := c.Query("month")
	if len(month) < 7 {
		month = time.Now().Format("2006-01")
	}
	year, open, diff, alloc, others, clos, err := buildNAS(month)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	f := func(v int64) float64 { return centsToYuan(v) }
	total := func(v []int64) float64 { return f(v[0] + v[1] + v[2]) }
	rows := []nasRowOut{
		{Label: "上年年末余额（本年年初余额）", Cols: []float64{f(open[0]), f(open[1]), f(open[2]), total(open)}},
		{Label: "本年变动：本年收支差额", Cols: []float64{f(diff[0]), f(diff[1]), f(diff[2]), total(diff)}},
		{Label: "本年变动：本年分配累计收益", Cols: []float64{f(alloc[0]), f(alloc[1]), f(alloc[2]), total(alloc)}},
		{Label: "本年变动：其他（划转/调整）", Cols: []float64{f(others[0]), f(others[1]), f(others[2]), total(others)}},
		{Label: "本年年末余额", Cols: []float64{f(clos[0]), f(clos[1]), f(clos[2]), total(clos)}},
	}
	diags := []diagItem{}
	if len(statementDiagnostics(year+"-12")) > 0 {
		var closedCnt int
		db.QueryRow(`SELECT COUNT(*) FROM periods WHERE month=?`, year+"-12").Scan(&closedCnt)
		if closedCnt == 0 {
			diags = append(diags, diagItem{Type: "unclosed", Level: "warn",
				Message: year + " 年度尚未全部结转：年末余额未包含未结转收支，'其他变动'行将显示相应差额，执行年度结转后归零。"})
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"year": year, "title": year + " 年度", "org": orgName(),
		"cols": []string{"商品住宅维修资金", "已售公有住房维修资金", "待分配累计收益", "净资产合计"},
		"rows": rows, "diagnostics": diags,
	})
}
