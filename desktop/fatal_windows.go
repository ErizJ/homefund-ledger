//go:build windows

package main

import (
	"fmt"
	"log"
	"os"
	"syscall"
	"unsafe"
)

// fatal 记录日志后弹窗提示并退出。
// 发布版用 -H=windowsgui 隐藏了控制台，错误必须用弹窗呈现给用户。
func fatal(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("[致命错误] %s", msg)
	user32 := syscall.NewLazyDLL("user32.dll")
	proc := user32.NewProc("MessageBoxW")
	title, _ := syscall.UTF16PtrFromString("住房维修基金记账系统")
	text, _ := syscall.UTF16PtrFromString(msg + "\n\n详细日志见程序目录下 data/vfund.log")
	proc.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), 0x10 /*MB_ICONERROR*/)
	os.Exit(1)
}
