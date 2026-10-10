//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

// webView2Installed 检测 WebView2 运行时（Evergreen）是否可用。
// Win10 部分机器未预装（Win11 内置），缺失时 wails 启动会弹英文错误。
func webView2Installed() bool {
	// 1. 注册表（官方安装的 Evergreen 运行时）
	for _, key := range []string{
		`SOFTWARE\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`,
		`SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`,
	} {
		if k, err := registry.OpenKey(registry.LOCAL_MACHINE, key, registry.QUERY_VALUE); err == nil {
			v, _, err := k.GetStringValue("pv")
			k.Close()
			if err == nil && v != "" {
				return true
			}
		}
	}
	// 2. 常见安装路径兜底
	pf := os.Getenv("ProgramFiles(x86)")
	if pf == "" {
		pf = os.Getenv("ProgramFiles")
	}
	if pf != "" {
		matches, _ := filepath.Glob(filepath.Join(pf, `Microsoft\EdgeWebView\Application\*\msedgewebview2.exe`))
		if len(matches) > 0 {
			return true
		}
	}
	return false
}

// showWebView2MissingDialog 中文提示缺组件，并打开微软官方下载页
func showWebView2MissingDialog() {
	user32 := syscall.NewLazyDLL("user32.dll")
	proc := user32.NewProc("MessageBoxW")
	title, _ := syscall.UTF16PtrFromString("住房维修基金记账系统")
	text, _ := syscall.UTF16PtrFromString(
		"检测到本机缺少「WebView2 运行时」组件（Windows 10 部分电脑未预装）。\n\n" +
			"点击「确定」将打开微软官方下载页面，安装完成后重新打开本软件即可。\n" +
			"（无法联网的电脑请联系管理员获取离线安装包）")
	proc.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), 0x40 /*MB_ICONINFORMATION*/)
	// 官方 Evergreen 引导安装器直链
	exec.Command("rundll32", "url.dll,FileProtocolHandler",
		"https://go.microsoft.com/fwlink/p/?LinkId=2124703").Start()
}
