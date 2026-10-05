package app

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

// Run 启动完整服务（命令行入口与桌面版共用）。
// dbPath：数据库文件路径，uploads/、reports/ 目录自动建在其同级；
// port：监听端口；www：可选，非 nil 时托管前端静态资源（SPA 回退 index.html），
// nil 表示纯 API 模式（开发时前端由 vite 托管）。
func Run(dbPath string, port int, www fs.FS) error {
	uploadsDir = filepath.Join(filepath.Dir(dbPath), "uploads")
	if err := os.MkdirAll(uploadsDir, 0o755); err != nil {
		return fmt.Errorf("创建附件目录失败: %w", err)
	}
	reportsDir = filepath.Join(filepath.Dir(dbPath), "reports")
	if err := os.MkdirAll(reportsDir, 0o755); err != nil {
		return fmt.Errorf("创建报表目录失败: %w", err)
	}
	if err := openDB(dbPath); err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}

	r := buildRouter(www)

	log.Printf("服务已启动: http://127.0.0.1:%d", port)
	return r.Run(fmt.Sprintf("127.0.0.1:%d", port))
}

// buildRouter 构建完整路由（www 非 nil 时托管前端静态资源，SPA 回退 index.html）。
// 独立成函数便于用 httptest 覆盖桌面版静态托管路径。
func buildRouter(www fs.FS) *gin.Engine {
	r := gin.Default()
	r.Use(corsMiddleware())

	// 登录/注销无需认证；其余 /api（含会话查询）全部需要登录
	r.POST("/api/login", authLogin)
	r.POST("/api/logout", authLogout)

	api := r.Group("/api")
	api.Use(authMiddleware())
	{
		api.GET("/session", authSession)
		api.GET("/settings", getSettings)
		api.PUT("/settings", updateSettings)
		api.GET("/communities", listCommunities)
		api.POST("/communities", createCommunity)
		api.PUT("/communities/:id", updateCommunity)
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
		api.POST("/vouchers/import", importVouchers)
		api.POST("/vouchers/expense-preview", previewExpense)
		api.POST("/vouchers/:id/void", voidVoucher)

		api.POST("/vouchers/:id/attachments", uploadAttachment)
		api.GET("/attachments/:id/download", downloadAttachment)
		api.DELETE("/attachments/:id", deleteAttachment)

		api.GET("/periods", listPeriods)
		api.POST("/periods/close", closePeriod)
		api.POST("/periods/reopen", reopenPeriod)
		api.GET("/periods/years", listFiscalYears)
		api.POST("/periods/close-year", closeFiscalYear)
		api.POST("/periods/reopen-year", reopenFiscalYear)

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
		api.POST("/gl/subjects", glSubjectCreate)
		api.PUT("/gl/subjects/:code", glSubjectUpdate)
		api.DELETE("/gl/subjects/:code", glSubjectDelete)
		api.GET("/gl/balances", glBalances)
		api.GET("/gl/entries", glEntries)
		api.GET("/gl/trial-balance", glTrialBalance)
		api.GET("/gl/general-ledger", glGeneralLedger)
		api.GET("/gl/balance-sheet", glBalanceSheet)
		api.GET("/gl/income-statement", glIncomeStatement)
		api.GET("/gl/net-asset-statement", glNetAssetStatement)
		api.GET("/gl/balance-sheet/pdf", glBalanceSheetPDF)
		api.GET("/gl/income-statement/pdf", glIncomeStatementPDF)
		api.GET("/gl/net-asset-statement/pdf", glNetAssetStatementPDF)
		api.GET("/gl/reconcile", glReconcile)
		api.POST("/gl/opening-balance", glOpeningBalance)
		api.GET("/gl/vouchers", glVoucherList)
		api.GET("/gl/vouchers/:id", glVoucherDetail)
		api.POST("/gl/manual-voucher", glManualVoucher)
		api.POST("/gl/vouchers/:id/void", glVoidManual)
		api.POST("/gl/backfill", glBackfill)
		api.POST("/gl/transfer", glTransferMonth)
		api.GET("/gl/voucher-summary", glVoucherSummary)
	}

	if www != nil {
		r.NoRoute(spaHandler(www))
	}
	return r
}
