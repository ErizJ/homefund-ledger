// 桌面版（Windows）壳：内嵌后端 + 前端静态资源，开原生窗口加载本地服务。
// 业务代码 100% 来自 vfund/server/app 与 web/dist，本目录不含任何业务逻辑。
package main

import (
	"context"
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
	"github.com/wailsapp/wails/v2/pkg/runtime"

	server "vfund/server/app"
)

// appVersion 与 versioninfo.json 保持一致，显示在窗口标题
const appVersion = "v1.0.0"

// webdist/dist 为 web/ 前端构建产物（npm run build 后拷贝到本目录，构建步骤见 docs/桌面版同步指南.md）
//
//go:embed webdist/dist
var webAssets embed.FS

// freePort 取 127.0.0.1 上的空闲端口，避免与用户已有 8080 服务冲突
func freePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fatal("获取空闲端口失败: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func main() {
	exe, err := os.Executable()
	if err != nil {
		fatal("定位程序目录失败: %v", err)
	}
	baseDir := filepath.Dir(exe)
	dbPath := filepath.Join(baseDir, "data", "vfund.db")

	// 日志写 data/vfund.log（发布版无控制台，排障看这里）
	logPath := filepath.Join(baseDir, "data", "vfund.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err == nil {
		if lf, err2 := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err2 == nil {
			log.SetOutput(io.MultiWriter(os.Stdout, lf))
			defer lf.Close()
		}
	}

	www, err := fs.Sub(webAssets, "webdist/dist")
	if err != nil {
		fatal("加载前端资源失败: %v", err)
	}

	// 后端：gin 托管前端静态资源 + /api（同一来源，登录 Cookie 与接口零改动）
	port := freePort()
	go func() {
		if err := server.Run(dbPath, port, www); err != nil {
			fatal("后端服务异常退出: %v", err)
		}
	}()

	// 窗口：初始加载一个运行时生成的重定向页，跳到本地 gin 服务
	err = wails.Run(&options.App{
		Title:     fmt.Sprintf("住房维修基金记账系统 %s", appVersion),
		Width:     1440,
		Height:    900,
		MinWidth:  1024,
		MinHeight: 700,
		AssetServer: &assetserver.Options{
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><script>location.replace('http://127.0.0.1:%d/')</script>`, port)
			}),
		},
		// 存在未结转月份时，关窗先确认（不锁账模型下极易忘记结转，给一次提醒）
		OnBeforeClose: func(ctx context.Context) bool {
			if !server.HasUnclosedMonths() {
				return false
			}
			res, err := runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
				Type:          runtime.QuestionDialog,
				Title:         "退出确认",
				Message:       "存在未结转的月份。退出后仍需在下次登录后完成结转，确定要退出吗？",
				Buttons:       []string{"继续退出", "返回"},
				DefaultButton: "返回",
				CancelButton:  "返回",
			})
			if err != nil {
				log.Printf("关闭确认弹窗失败: %v", err)
				return false
			}
			return res != "继续退出"
		},
	})
	if err != nil {
		fatal("启动窗口失败: %v", err)
	}
}
