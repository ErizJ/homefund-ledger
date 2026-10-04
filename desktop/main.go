// 桌面版（Windows）壳：内嵌后端 + 前端静态资源，开原生窗口加载本地服务。
// 业务代码 100% 来自 vfund/server/app 与 web/dist，本目录不含任何业务逻辑。
package main

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	server "vfund/server/app"
)

// webdist/dist 为 web/ 前端构建产物（npm run build 后拷贝到本目录，构建步骤见 docs/桌面版同步指南.md）
//
//go:embed webdist/dist
var webAssets embed.FS

// freePort 取 127.0.0.1 上的空闲端口，避免与用户已有 8080 服务冲突
func freePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("获取空闲端口失败: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func main() {
	exe, err := os.Executable()
	if err != nil {
		log.Fatalf("定位程序目录失败: %v", err)
	}
	baseDir := filepath.Dir(exe)
	dbPath := filepath.Join(baseDir, "data", "vfund.db")

	// 日志同时写 data/vfund.log 与控制台（首版保留控制台便于排障）
	logPath := filepath.Join(baseDir, "data", "vfund.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err == nil {
		if lf, err2 := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err2 == nil {
			log.SetOutput(io.MultiWriter(os.Stdout, lf))
			defer lf.Close()
		}
	}

	www, err := fs.Sub(webAssets, "webdist/dist")
	if err != nil {
		log.Fatalf("加载前端资源失败: %v", err)
	}

	// 后端：gin 托管前端静态资源 + /api（同一来源，登录 Cookie 与接口零改动）
	port := freePort()
	go func() {
		if err := server.Run(dbPath, port, www); err != nil {
			log.Printf("后端服务退出: %v", err)
		}
	}()

	// 窗口：初始加载一个运行时生成的重定向页，跳到本地 gin 服务
	err = wails.Run(&options.App{
		Title:     "住房维修基金记账系统",
		Width:     1440,
		Height:    900,
		MinWidth:  1024,
		MinHeight: 700,
		AssetServer: &assetserver.Options{
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><script>location.replace('http://127.0.0.1:%d/')</script>`, port)
			}),
		},
	})
	if err != nil {
		log.Fatalf("启动窗口失败: %v", err)
	}
}
