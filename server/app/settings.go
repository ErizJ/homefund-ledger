package app

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// orgName 当前编制单位名称（默认"演示代管单位"，可在基础数据页修改）
func orgName() string {
	var v string
	db.QueryRow(`SELECT value FROM settings WHERE key='org_name'`).Scan(&v)
	if strings.TrimSpace(v) == "" {
		return "演示代管单位"
	}
	return strings.TrimSpace(v)
}

// displayName 登录用户名 → 显示名称（现阶段 mock：admin → 系统管理员）
func displayName(user string) string {
	if user == "admin" {
		return "系统管理员"
	}
	return user
}

// shouldAutoGL 日常录入是否自动生成汇总记账凭证（默认开）
func shouldAutoGL() bool {
	var v string
	db.QueryRow(`SELECT value FROM settings WHERE key='auto_gl'`).Scan(&v)
	return v != "0"
}

// GET /api/settings
func getSettings(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"orgName": orgName(), "autoGl": shouldAutoGL()})
}

// PUT /api/settings  {orgName}
func updateSettings(c *gin.Context) {
	var req struct {
		OrgName *string `json:"orgName"`
		AutoGl  *bool   `json:"autoGl"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误：" + err.Error()})
		return
	}
	if req.AutoGl != nil {
		v := "1"
		if !*req.AutoGl {
			v = "0"
		}
		if _, err := db.Exec(`INSERT INTO settings(key, value) VALUES('auto_gl', ?)
			ON CONFLICT(key) DO UPDATE SET value=excluded.value`, v); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	if req.OrgName != nil {
		if strings.TrimSpace(*req.OrgName) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "编制单位名称不能为空"})
			return
		}
		if _, err := db.Exec(`INSERT INTO settings(key, value) VALUES('org_name', ?)
			ON CONFLICT(key) DO UPDATE SET value=excluded.value`, strings.TrimSpace(*req.OrgName)); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "orgName": orgName()})
}
