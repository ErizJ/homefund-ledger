package app

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// spaHandler 桌面版静态资源托管：优先按路径找文件，找不到回退 index.html（前端路由）。
// 仅在 Run 传入 www 时挂到 NoRoute，开发模式（www=nil）行为不变。
func spaHandler(www fs.FS) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodGet || strings.HasPrefix(c.Request.URL.Path, "/api") {
			c.JSON(http.StatusNotFound, gin.H{"error": "接口不存在"})
			return
		}
		p := strings.TrimPrefix(c.Request.URL.Path, "/")
		if p != "" {
			if f, err := www.Open(p); err == nil {
				if st, err2 := f.Stat(); err2 == nil && !st.IsDir() {
					f.Close()
					c.FileFromFS(p, http.FS(www))
					return
				}
				f.Close()
			}
		}
		c.FileFromFS("index.html", http.FS(www))
	}
}
