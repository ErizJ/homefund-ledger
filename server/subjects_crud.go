package main

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// ==================== 会计科目自定义管理 ====================
// 自定义科目会实时进入：手工凭证科目选择、明细账、科目余额表、总账、
// 以及三张财务报表（动态行；子科目编码 = 上级+01 归商品住宅栏、+02 归公有住房栏）。

var subjectCodeRe = regexp.MustCompile(`^\d{2,6}$`)

var subjectTypes = map[string]bool{
	"asset": true, "liability": true, "net_asset": true, "income": true, "expense": true,
}

// POST /api/gl/subjects  {code, name, type, parent?}
func glSubjectCreate(c *gin.Context) {
	var req struct {
		Code   string `json:"code" binding:"required"`
		Name   string `json:"name" binding:"required"`
		Type   string `json:"type" binding:"required"`
		Parent string `json:"parent"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误：" + err.Error()})
		return
	}
	code := strings.TrimSpace(req.Code)
	name := strings.TrimSpace(req.Name)
	if !subjectCodeRe.MatchString(code) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "科目编码须为 2-6 位数字（子科目建议 = 上级编码 + 01/02，便于报表分栏）"})
		return
	}
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "科目名称不能为空"})
		return
	}
	if !subjectTypes[req.Type] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "科目类型必须为 asset / liability / net_asset / income / expense"})
		return
	}
	parent := strings.TrimSpace(req.Parent)
	if parent != "" {
		var pType, pParent string
		if err := db.QueryRow(`SELECT type, parent FROM gl_subjects WHERE code=?`, parent).Scan(&pType, &pParent); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "上级科目不存在：" + parent})
			return
		}
		if pParent != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "科目层级最多两级：上级科目必须是一级科目"})
			return
		}
		if pType != req.Type {
			c.JSON(http.StatusBadRequest, gin.H{"error": "子科目类型须与上级科目一致（" + pType + "）"})
			return
		}
	}
	var cnt int
	db.QueryRow(`SELECT COUNT(*) FROM gl_subjects WHERE code=?`, code).Scan(&cnt)
	if cnt > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "科目编码已存在：" + code})
		return
	}
	if _, err := db.Exec(`INSERT INTO gl_subjects(code, name, type, parent, enabled) VALUES(?,?,?,?,1)`,
		code, name, req.Type, parent); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "code": code})
}

// PUT /api/gl/subjects/:code  {name?, code?, enabled?}
// 修改编码时同步改写 gl_entries 引用与子科目 parent（需临时关闭外键检查）
func glSubjectUpdate(c *gin.Context) {
	oldCode := c.Param("code")
	var req struct {
		Name    *string `json:"name"`
		Code    *string `json:"code"`
		Enabled *bool   `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误：" + err.Error()})
		return
	}
	var name, typ, parent string
	var enabled int
	if err := db.QueryRow(`SELECT name, type, parent, enabled FROM gl_subjects WHERE code=?`, oldCode).Scan(&name, &typ, &parent, &enabled); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "科目不存在：" + oldCode})
		return
	}
	newCode := oldCode
	rename := false
	if req.Code != nil && strings.TrimSpace(*req.Code) != "" && *req.Code != oldCode {
		newCode = strings.TrimSpace(*req.Code)
		if !subjectCodeRe.MatchString(newCode) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "科目编码须为 2-6 位数字"})
			return
		}
		var cnt int
		db.QueryRow(`SELECT COUNT(*) FROM gl_subjects WHERE code=?`, newCode).Scan(&cnt)
		if cnt > 0 {
			c.JSON(http.StatusConflict, gin.H{"error": "科目编码已存在：" + newCode})
			return
		}
		rename = true
	}
	newName := name
	if req.Name != nil && strings.TrimSpace(*req.Name) != "" {
		newName = strings.TrimSpace(*req.Name)
	}
	newEnabled := enabled
	if req.Enabled != nil {
		if *req.Enabled {
			newEnabled = 1
		} else {
			newEnabled = 0
		}
	}

	if rename {
		// 子科目编码随上级一并迁移（子科目 = 上级编码 + 后缀，如 610101 → 620101）
		childRows, err := db.Query(`SELECT code FROM gl_subjects WHERE parent=?`, oldCode)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		type childRen struct{ old, new string }
		children := []childRen{}
		for childRows.Next() {
			var ch string
			childRows.Scan(&ch)
			children = append(children, childRen{old: ch, new: newCode + strings.TrimPrefix(ch, oldCode)})
		}
		childRows.Close()

		db.SetMaxOpenConns(1)
		defer db.SetMaxOpenConns(0)
		if _, err := db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer db.Exec(`PRAGMA foreign_keys=ON`)
		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer tx.Rollback()
		if _, err := tx.Exec(`UPDATE gl_entries SET subject_code=? WHERE subject_code=?`, newCode, oldCode); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		for _, ch := range children {
			if _, err := tx.Exec(`UPDATE gl_entries SET subject_code=? WHERE subject_code=?`, ch.new, ch.old); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			if _, err := tx.Exec(`UPDATE gl_subjects SET code=? WHERE code=?`, ch.new, ch.old); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}
		if _, err := tx.Exec(`UPDATE gl_subjects SET parent=? WHERE parent=?`, newCode, oldCode); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if _, err := tx.Exec(`UPDATE gl_subjects SET code=?, name=?, type=?, enabled=? WHERE code=?`,
			newCode, newName, typ, newEnabled, oldCode); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if err := tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	} else {
		if _, err := db.Exec(`UPDATE gl_subjects SET name=?, enabled=? WHERE code=?`,
			newName, newEnabled, oldCode); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "code": newCode})
}

// DELETE /api/gl/subjects/:code  仅未使用（无分录、无子科目）的科目可删除
func glSubjectDelete(c *gin.Context) {
	code := c.Param("code")
	var cnt int
	db.QueryRow(`SELECT COUNT(*) FROM gl_entries WHERE subject_code=?`, code).Scan(&cnt)
	if cnt > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "该科目已有 " + strconv.Itoa(cnt) + " 条分录，不能删除；如需停用请点「停用」"})
		return
	}
	db.QueryRow(`SELECT COUNT(*) FROM gl_subjects WHERE parent=?`, code).Scan(&cnt)
	if cnt > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "该科目下还有子科目，请先处理子科目"})
		return
	}
	res, err := db.Exec(`DELETE FROM gl_subjects WHERE code=?`, code)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "科目不存在：" + code})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
