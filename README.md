# Vela

基于 Wails v3、React 和 mihomo 的 macOS 代理客户端。当前已初始化官方 React + TypeScript 示例，包含前端调用 Go 的问候按钮及 Go 推送的时钟事件；代理业务、托盘和辅助服务尚未实现。

## 环境

- Go 1.25 或更新版本（本机验证使用 Go 1.26.5）。
- Node.js 24 与 npm（本机验证使用 Node.js 24.14.0）。
- macOS Command Line Tools：`xcode-select --install`。
- Wails CLI 和前端 runtime 均锁定为 `v3.0.0-beta.24`。

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.24
export PATH="$(go env GOPATH)/bin:$PATH"
```

## 开发运行

在项目根目录运行：

```sh
wails3 dev
```

官方任务会安装前端依赖、生成绑定、启动 Vite 和桌面窗口。Vite 使用 `127.0.0.1:9245`。在桌面窗口中输入名字并点击 Greet，可调用 Go 的 `GreetService.Greet`；底部时钟验证 Go 到前端的事件推送。

## 构建与检查

```sh
wails3 package
go test ./...
go vet ./...
npm --prefix frontend run typecheck
open bin/vela.app
```

打包产物为 `bin/vela.app`，使用本机 ad-hoc 签名，不是公开分发用的公证版本。前端生成绑定与构建产物不提交；提交 `go.sum` 和 `frontend/package-lock.json`。需要严格复现前端安装时在 `frontend` 目录运行 `npm ci`。

## 目录与设计

根目录 `main.go`、`greetservice.go` 暂时保留官方演示结构；业务实现时按设计逐步迁入 `internal/app` 与 `internal/desktop`。官方模板附带的其他平台构建文件保留，首版只验证 macOS。

- [首版功能与技术设计](docs/design/macos-proxy-client-design.md)
- [项目架构、目录与依赖约定](docs/design/project-structure.md)
