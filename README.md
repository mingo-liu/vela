# Vela

Vela 是基于 Wails v3、React 和 mihomo 的 macOS 本地代理客户端。当前 MVP 支持导入单份 mihomo YAML 或 HTTP/HTTPS 订阅地址、手动更新订阅、启动与停止本地代理，以及选择手动策略组中的节点。关闭窗口后应用留在菜单栏，明确退出时停止内核。

**当前版本不修改 macOS 系统代理设置。** 本地 mixed 端口固定为 `127.0.0.1:7890`，需要在使用代理的应用中手动填入该地址。系统代理辅助服务、自动恢复、多配置、TUN 和 provider 缓存仍在后续阶段。

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

完整设计和后续实施顺序见本地的 `docs/design/`。该目录目前按项目规则不纳入 Git。
