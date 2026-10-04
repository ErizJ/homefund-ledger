// 命令行入口（开发模式）：./vfund-server [数据库路径]
// 桌面版通过 import "vfund/server/app" 复用同一套服务代码（见 desktop/）。
package main

import (
	"log"
	"os"
	"strconv"

	app "vfund/server/app"
)

func main() {
	dbPath := "vfund.db"
	if len(os.Args) > 1 {
		dbPath = os.Args[1]
	}
	port := 8080
	if p := os.Getenv("VFUND_PORT"); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			port = n
		}
	}
	if err := app.Run(dbPath, port, nil); err != nil {
		log.Fatalf("启动失败: %v", err)
	}
}
