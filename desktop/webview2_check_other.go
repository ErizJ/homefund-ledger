//go:build !windows

package main

// webView2Installed 非 Windows 平台无需 WebView2 检测（开发/交叉编译期占位）
func webView2Installed() bool {
	return true
}

// showWebView2MissingDialog 非 Windows 平台占位
func showWebView2MissingDialog() {}
