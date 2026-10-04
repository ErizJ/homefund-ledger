package app

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// POST /api/vouchers/:id/attachments  multipart 字段名 file
func uploadAttachment(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效凭证 id"})
		return
	}
	var vdate string
	if err := db.QueryRow(`SELECT date FROM vouchers WHERE id=?`, id).Scan(&vdate); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "凭证不存在"})
		return
	}
	// 月结/年结锁账校验：锁账月份的凭证禁止上传附件
	if msg := lockError(vdate[:7]); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": msg + "，请先解锁再上传附件"})
		return
	}
	fh, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请通过 multipart 字段 file 上传文件"})
		return
	}
	if fh.Size > 20*1024*1024 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "单个附件不能超过 20MB"})
		return
	}
	stored := fmt.Sprintf("v%d_%d%s", id, time.Now().UnixNano(), filepath.Ext(fh.Filename))
	if err := c.SaveUploadedFile(fh, filepath.Join(uploadsDir, stored)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存文件失败：" + err.Error()})
		return
	}
	res, err := db.Exec(`INSERT INTO attachments(voucher_id, filename, stored_name, size, created_at) VALUES(?,?,?,?,?)`,
		id, fh.Filename, stored, fh.Size, time.Now().Format(time.RFC3339))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	aid, _ := res.LastInsertId()
	c.JSON(http.StatusOK, gin.H{"id": aid, "filename": fh.Filename, "size": fh.Size})
}

// GET /api/attachments/:id/download
func downloadAttachment(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效 id"})
		return
	}
	var filename, stored string
	if err := db.QueryRow(`SELECT filename, stored_name FROM attachments WHERE id=?`, id).Scan(&filename, &stored); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "附件不存在"})
		return
	}
	path := filepath.Join(uploadsDir, stored)
	if _, err := os.Stat(path); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "附件文件已丢失"})
		return
	}
	c.FileAttachment(path, filename)
}

// DELETE /api/attachments/:id
func deleteAttachment(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效 id"})
		return
	}
	var stored, vdate string
	if err := db.QueryRow(`SELECT a.stored_name, v.date FROM attachments a
		LEFT JOIN vouchers v ON a.voucher_id=v.id WHERE a.id=?`, id).Scan(&stored, &vdate); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "附件不存在"})
		return
	}
	if len(vdate) >= 7 {
		if msg := lockError(vdate[:7]); msg != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": msg + "，请先解锁再删除附件"})
			return
		}
	}
	os.Remove(filepath.Join(uploadsDir, stored))
	if _, err := db.Exec(`DELETE FROM attachments WHERE id=?`, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
