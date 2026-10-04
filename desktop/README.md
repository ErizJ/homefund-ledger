# 桌面版（Windows）目录

本目录是桌面版（Windows 独立软件）的壳代码与打包配置。业务代码 100% 复用仓库的 `server/app` 与 `web/dist`，本目录**不复制任何业务逻辑**。

**当前状态：雏形版（2026-10-05 起可用）**

- `main.go`：内嵌后端 + 前端静态资源，启动 gin（127.0.0.1 随机端口）后用 Wails 开原生窗口跳转到本地服务
- 数据落在 exe 同级 `data/`（vfund.db、uploads/、reports/、vfund.log），备份 = 拷贝该目录
- 构建产物（vfund.exe、webdist/dist）不入库

## 构建 Windows exe

```bash
# 1. 前端构建（产物拷贝进壳内，供 go:embed 打包）
cd web && npm run build
cp -R web/dist desktop/webdist/dist        # Windows 下用文件管理器复制覆盖亦可

# 2. 打包
cd desktop
go build -o vfund.exe .                    # Windows 机器上
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o vfund.exe .   # macOS/Linux 交叉编译（已验证可行）
```

交付 = `vfund.exe` + 首次运行自动生成的 `data/` 目录；老用户升级只替换 exe。

## 尚未完成（后续打磨项）

- 窗口图标、exe 版本信息/数字签名、NSIS 安装包
- 隐藏启动时的控制台窗口（当前保留便于排障）
- 窗口关闭时提示未结转月份、禁用刷新快捷键
- GitHub Actions 自动出包（配置见 docs/桌面版同步指南.md 附录 A）

同步流程与开发守则见 [../docs/桌面版同步指南.md](../docs/桌面版同步指南.md)。

创建日期：2026-10-05
