# 桌面版（Windows）目录

本目录是桌面版（Windows 独立软件）的壳代码与打包配置。业务代码 100% 复用仓库的 `server/app` 与 `web/dist`，本目录**不复制任何业务逻辑**。

**当前状态：v1.0.0（2026-10-05）**

- `main.go`：内嵌后端 + 前端静态资源，启动 gin（127.0.0.1 随机端口）后用 Wails 开原生窗口跳转到本地服务
- 窗口图标、exe 版本信息、高清屏 manifest：由 `resource_windows_amd64.syso` 提供（生成见下）
- 无控制台窗口；致命错误弹窗提示（详细日志在 `data/vfund.log`）
- 关窗时若存在未结转月份，弹窗确认；桌面模式自动屏蔽 F5/Ctrl+R 刷新快捷键
- 数据落在 exe 同级 `data/`（vfund.db、uploads/、reports/、vfund.log），备份 = 拷贝该目录
- 构建产物（vfund.exe、webdist/dist）不入库

## 构建 Windows exe

```bash
# 1. 前端构建（产物拷贝进壳内，供 go:embed 打包）
cd web && npm run build
cp -R web/dist desktop/webdist/dist        # Windows 下用文件管理器复制覆盖亦可

# 2. 打包（-H=windowsgui 隐藏控制台；图标/版本信息由已提交的 resource_windows_amd64.syso 自动链接）
cd desktop
go build -ldflags "-H=windowsgui" -o vfund.exe .                    # Windows 机器上
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-H=windowsgui" -o vfund.exe .   # macOS/Linux 交叉编译（已验证）
```

交付 = `vfund.exe` + 首次运行自动生成的 `data/` 目录；老用户升级只替换 exe。

### 重新生成图标 / 版本资源

```bash
# 图标（蓝底"住"字，改字或改色后重跑）
cd desktop/tools/genicon && go run . ../../server/app/assets/fonts/NotoSansSC.ttf ../../icon.ico

# 版本信息/图标/manifest → resource_windows_amd64.syso（需先 go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest）
cd desktop && goversioninfo -64 -o resource_windows_amd64.syso versioninfo.json
```

版本号同时维护两处：`main.go` 的 `appVersion` 常量与 `versioninfo.json`。

## 尚未完成（后续打磨项）

- 数字签名（需购买代码签名证书）、NSIS 安装包
- GitHub Actions 自动出包已配置（[.github/workflows/windows-build.yml](../.github/workflows/windows-build.yml)，打 `v*` tag 触发），待首次实跑验证

同步流程与开发守则见 [../docs/桌面版同步指南.md](../docs/桌面版同步指南.md)。

创建日期：2026-10-05
