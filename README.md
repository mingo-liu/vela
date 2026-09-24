# Vela

Vela 是基于 Wails v3、React 和 mihomo 的 macOS 本地代理客户端。当前 MVP 支持导入单份 mihomo YAML 或 HTTP/HTTPS 订阅地址、手动更新订阅、选择手动策略组中的节点，以及系统代理和 Tun 两种网络设置。代理模式可选 Rule、Global 或 Direct。关闭窗口后应用留在菜单栏，明确退出时停止内核。

本地 mixed 端口默认是 `127.0.0.1:7890`，可在 Settings 中修改为 1024–65535 范围内的端口。运行中修改会重建当前连接，失败时尝试恢复原端口和连接。打开系统代理时，Vela 先启动内核，再让遵循 macOS 系统代理设置的应用通过 Vela 连接；关闭时先恢复原代理设置，再停止内核。独立监视进程会在 GUI 异常退出时尝试恢复。系统代理不覆盖不遵循该设置的应用或 UDP 流量。

Tun 模式首次使用或 Tun 服务 / mihomo 更新后会请求一次 macOS 管理员授权。Vela 将独立 Tun 服务和受信任的 mihomo 内核安装到 `/Library/PrivilegedHelperTools/local.vela.desktop.tun/`，由 launchd 管理服务；只更新 Vela 界面无需重新授权。后续开关 Tun 通过仅限授权用户访问的本地套接字与服务通信，不再弹出密码框。服务使用 mihomo 的 `auto-route` 接管设备流量，并启用内部 DNS 与 DNS 劫持。系统代理与 Tun 互斥；切换失败时 Vela 会尝试恢复原模式。关闭 Tun 或退出应用时会停止内核；GUI 异常退出后，服务会检测客户端断开并停止内核。macOS 对发往局域网 DNS 的请求有劫持限制。多配置和 provider 缓存仍在后续阶段。

如需移除已授权的 Tun 服务，先退出 Vela，再运行 `bin/vela --vela-tun-uninstall`（已打包应用可使用 `.app/Contents/MacOS/vela`）。

窗口左侧的首页（Home）显示连接状态、网络设置开关和代理模式选择。代理模式默认规则（Rule）；全局（Global）使用代理页的 GLOBAL 策略组，直连（Direct）让进入内核的流量直连。代理模式可以在未连接时预选，运行中切换会即时生效，并在重启或切换订阅后保持。代理页（Proxies）用于选择策略组节点、测量节点延迟并按名称或延迟排序，配置页（Profiles）用于导入 YAML 或管理订阅。未连接时测速会临时启动内核，完成后关闭，不改变网络设置。

Settings 可设置登录时启动、启动后自动连接及其连接方式、代理模式、日志级别和界面语言，并查看本地代理地址、内核名称与版本号，以及打开配置目录。界面语言可在简体中文与 English 之间切换，立即更新窗口和菜单栏，重启后保留选择。菜单栏的“设置…”也可打开该页面。登录时启动需要从已打包的 `Vela.app` 中开启；自动连接只在启动时有可用配置的情况下执行。日志级别选择“跟随配置”时保留 YAML 中的值，覆盖值在重新载入配置或下次连接时生效。

## 构建与运行

需要 macOS Apple Silicon、Go 1.25+、Node.js 24、npm、Xcode Command Line Tools，以及 `wails3 v3.0.0-beta.24`。

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.24
wails3 package
open bin/vela.app
```

打包任务从 mihomo 官方发布页下载固定的 `v1.19.31` arm64 文件，并从 MetaCubeX 规则库的固定提交下载 GeoIP 数据库；两者均校验 SHA-256 后放入 `.app/Contents/Resources/`。下载缓存位于 `build/resources/`，不会提交。当前 `.app` 使用本机 ad-hoc 签名，仅用于本机验收。

开发运行：

```sh
wails3 dev
```

如果只运行 Go 侧的真实内核集成测试：

```sh
VELA_TEST_MIHOMO="$PWD/build/resources/mihomo" go test ./internal/mihomo -run TestRunnerWithRealCore -v
```

常规检查：

```sh
go test ./...
go vet ./...
npm --prefix frontend run typecheck
```
