//go:build !windows

package main

import (
	"log"
	"os"
)

// fatal 非 Windows 平台仅记录日志并退出（开发/交叉编译期用）。
func fatal(format string, args ...any) {
	log.Printf("[致命错误] "+format, args...)
	os.Exit(1)
}
