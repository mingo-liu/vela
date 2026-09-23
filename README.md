# Vela

Vela 是基于 Wails v3、React 和 mihomo 的 macOS 本地代理客户端。当前 MVP 支持导入单份 mihomo YAML 或 HTTP/HTTPS 订阅地址、手动更新订阅、启动与停止本地代理、选择手动策略组中的节点，以及接管当前网络服务的系统代理。关闭窗口后应用留在菜单栏，明确退出时停止内核。

本地 mixed 端口固定为 `127.0.0.1:7890`。启动内核后，使用界面或菜单栏的“开启系统代理”即可让遵循 macOS 系统代理设置的应用通过 Vela 连接。关闭系统代理、停止内核或正常退出时，Vela 会恢复启用前的代理设置。独立监视进程会在 GUI 异常退出时尝试恢复。系统代理不覆盖不遵循该设置的应用或 UDP 流量；TUN、多配置和 provider 缓存仍在后续阶段。

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

## 导入范围与数据

- 接受单文档、最多 2 MiB 的 mihomo YAML。当前支持内联节点、策略组、规则、GEOIP 规则与基本 DNS 配置；额外监听、TUN、外部 provider、GEOSITE 等其他 GEO 规则及未验证的顶层字段会明确拒绝。
- 订阅下载使用直连，不继承系统代理；限制超时、响应大小和重定向。请求使用 Clash.Meta 客户端标识，以便支持按客户端类型返回 mihomo YAML 的订阅服务。下载或校验失败时，旧配置保持不变。仅接受返回 mihomo YAML 的订阅地址；如果服务端仍返回 Base64 节点列表，界面会提示切换订阅格式，当前版本不转换节点列表。
- 订阅 URL 存在 macOS Keychain，配置正文保存在 `~/Library/Application Support/Vela/` 的私有文件中。URL 中的令牌不会写进仓库或应用日志。HTTP 订阅会在网络上传输明文令牌，优先使用服务商提供的 HTTPS 地址。
- 运行时控制接口只监听回环地址，使用每次启动生成的随机密钥。界面不直接访问控制接口。
- 系统代理只修改当前默认网络接口对应的服务，保存原有 HTTP、HTTPS、SOCKS、PAC 与自动发现设置。关闭时逐字段确认仍指向 Vela 才恢复，避免覆盖其他应用后续的修改。切换 Wi-Fi 或有线服务后，需要先关闭再开启系统代理以接管新服务；需要认证的已有代理不会被接管。修改网络设置需要当前用户具备 macOS 管理员权限；授权失败时，本地代理仍可使用。

完整设计和后续实施顺序见本地的 `docs/design/`。该目录目前按项目规则不纳入 Git。
