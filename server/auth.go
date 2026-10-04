package main

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// 单机单用户登录：账号密码默认 admin/admin，可用环境变量 VFUND_USER / VFUND_PASS 覆盖。
// 会话存内存（token → 用户），24 小时有效；重启服务后需重新登录。
type authSessionRec struct {
	user      string
	createdAt time.Time
}

var (
	authSessions   = map[string]authSessionRec{}
	authSessionsMu sync.Mutex
)

const authTTL = 24 * time.Hour

func authConfig() (string, string) {
	user := os.Getenv("VFUND_USER")
	pass := os.Getenv("VFUND_PASS")
	if user == "" {
		user = "admin"
	}
	if pass == "" {
		pass = "admin"
	}
	return user, pass
}

func newAuthToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// POST /api/login  {username, password} → 设置会话 Cookie
func authLogin(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请输入用户名和密码"})
		return
	}
	user, pass := authConfig()
	if req.Username != user || req.Password != pass {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}
	token, err := newAuthToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成会话失败"})
		return
	}
	authSessionsMu.Lock()
	authSessions[token] = authSessionRec{user: user, createdAt: time.Now()}
	authSessionsMu.Unlock()
	c.SetCookie("vf_token", token, int(authTTL.Seconds()), "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"ok": true, "user": user,
		"displayName": displayName(user), "org": orgName()})
}

// POST /api/logout  注销当前会话
func authLogout(c *gin.Context) {
	if token, err := c.Cookie("vf_token"); err == nil {
		authSessionsMu.Lock()
		delete(authSessions, token)
		authSessionsMu.Unlock()
	}
	c.SetCookie("vf_token", "", -1, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// GET /api/session  当前登录用户（未登录返回 401，前端据此显示登录页）
func authSession(c *gin.Context) {
	user, ok := c.Get("authUser")
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	u := user.(string)
	c.JSON(http.StatusOK, gin.H{"user": u, "displayName": displayName(u), "org": orgName()})
}

// authMiddleware 校验会话 Cookie，通过后在上下文中记录用户
func authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie("vf_token")
		if err == nil && token != "" {
			authSessionsMu.Lock()
			s, ok := authSessions[token]
			if ok && time.Since(s.createdAt) > authTTL {
				delete(authSessions, token)
				ok = false
			}
			authSessionsMu.Unlock()
			if ok {
				c.Set("authUser", s.user)
				c.Next()
				return
			}
		}
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "未登录或会话已过期"})
	}
}
