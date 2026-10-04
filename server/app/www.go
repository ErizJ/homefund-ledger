package app

import (
	"bytes"
	"io/fs"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

// desktopGuardScript 桌面模式下注入 index.html：屏蔽刷新快捷键
// （桌面窗口没有"刷新页面"概念，误按 F5/Ctrl+R 会丢编辑态）
const desktopGuardScript = `<script>window.__VFUND_DESKTOP__=1;document.addEventListener('keydown',function(e){if(e.key==='F5'||((e.ctrlKey||e.metaKey)&&(e.key==='r'||e.key==='R'))){e.preventDefault();e.stopPropagation();}},true);</script>`

var (
	indexOnce sync.Once
	indexHTML []byte
)

// servedIndex 返回注入桌面防护脚本后的 index.html（首次读取后缓存）。
func servedIndex(www fs.FS) []byte {
	indexOnce.Do(func() {
		b, err := fs.ReadFile(www, "index.html")
		if err != nil {
			indexHTML = []byte("<!doctype html><html><body>前端资源缺失，请重新打包</body></html>")
			return
		}
		indexHTML = bytes.Replace(b, []byte("</head>"), append([]byte(desktopGuardScript), []byte("</head>")...), 1)
	})
	return indexHTML
}

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
		c.Data(http.StatusOK, "text/html; charset=utf-8", servedIndex(www))
	}
}
