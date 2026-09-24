# Vela

[English](README.md) · [简体中文](README.zh-CN.md)

Vela 是基于 Wails v3、React 和 mihomo 构建的 macOS 代理客户端。它提供桌面界面，用于管理 mihomo 配置，并通过 macOS 系统代理或 Tun 模式转发流量。

## 功能

- 导入本地 mihomo YAML 文件或 HTTP/HTTPS 订阅，手动更新订阅并切换当前配置。
- 在手动策略组中选择节点、测试延迟，并按名称或延迟排序。
- 切换 Rule、Global 和 Direct 代理模式。
- 使用系统代理或 Tun 模式连接，两种模式互斥。
- 设置启动行为、本地代理端口、日志级别和界面语言。关闭窗口后应用留在菜单栏；退出应用时停止内核。

## 构建与运行

需要 Apple Silicon Mac、Go 1.25+、Node.js 24、npm、Xcode Command Line Tools，以及 Wails。

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.24
wails3 package
open bin/Vela.app
```

开发运行使用 `wails3 dev`。

## 网络模式

**系统代理**会启动 mihomo 并更新 macOS 代理设置。它只影响遵循该设置的应用，不覆盖忽略系统代理的应用或 UDP 流量。断开连接时 Vela 会恢复原设置；GUI 异常退出时也会尝试恢复。


本地 mixed 代理默认监听 `127.0.0.1:7890`。

如需移除已授权的 Tun 服务，先退出 Vela，再运行：

```sh
bin/vela --vela-tun-uninstall
```

## 检查

```sh
go test ./...
go vet ./...
npm --prefix frontend run typecheck
```

## 许可证

[MIT](LICENSE)
