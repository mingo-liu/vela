<p align="center">
  <img src="build/appicon.png" alt="Vela 图标" width="200">
</p>

<h1 align="center">Vela</h1>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-green" alt="MIT 许可证"></a>
  <a href="https://github.com/mingo-liu/vela/actions/workflows/test.yml"><img src="https://github.com/mingo-liu/vela/actions/workflows/test.yml/badge.svg" alt="测试状态"></a>
  <img src="https://img.shields.io/badge/macOS-Apple_Silicon-black?logo=apple" alt="macOS Apple Silicon">
  <img src="https://img.shields.io/badge/Go-1.26.8-00ADD8?logo=go&amp;logoColor=white" alt="Go 1.26.8">
  <img src="https://img.shields.io/badge/Wails-v3-blue" alt="Wails v3">
  <img src="https://img.shields.io/badge/React-18-149ECA?logo=react&amp;logoColor=white" alt="React 18">
</p>

<p align="center">
  <a href="README.md">English</a> · <a href="README.zh-CN.md">简体中文</a>
</p>

Vela 是基于 Wails v3、React 和 mihomo 构建的 macOS 代理客户端。它提供桌面界面，用于管理 mihomo 配置，并通过 macOS 系统代理或 Tun 模式转发流量。

## 功能

- 导入本地 mihomo YAML 文件或 HTTP/HTTPS 订阅，编辑和删除订阅、定时更新订阅并切换当前配置。
- 支持配置中的节点提供器、规则提供器、Sniffer、GeoIP 和 GeoSite 规则；本地节点提供器文件须放在配置目录的 `providers/` 或 `proxies/` 下。
- TUN 模式支持内联和本地节点提供器；HTTP 节点提供器仅用于系统代理模式。
- 在手动策略组中选择节点（含节点提供器）、测试延迟，并按名称或延迟排序；订阅更新后保留仍然有效的节点选择。
- 订阅下载和节点测速显示任务进度并支持取消；测速期间可断开连接。
- 切换 Rule、Global 和 Direct 代理模式。
- 使用系统代理或 Tun 模式连接，两种模式互斥。
- 设置启动行为、本地代理端口、日志级别和界面语言。关闭窗口后应用留在菜单栏；退出应用时停止内核。
- 在诊断页查看活动连接、代理流量、代理链和已加载的规则。
- 在订阅卡片的“编辑配置”中查看原有规则，并按完整域名或域名后缀添加直连、阻断或现有策略组规则，支持编辑、排序、启用和删除。自定义规则按订阅独立保存，优先于原有规则且不受订阅更新影响；切换订阅时使用各自的规则，仅在 Rule 模式生效。编辑未使用的订阅不会切换当前配置；当前配置缺少目标策略组时，该规则保留但暂不生效。本地配置也提供规则编辑入口。
- 窗口隐藏或最小化时暂停界面定时刷新，重新显示时立即刷新；代理连接继续运行。

## 构建与运行

需要 Apple Silicon Mac、Go 1.26.8+、Node.js 24、npm、Xcode Command Line Tools，以及 Wails。

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.24
wails3 package
open bin/Vela.app
```

开发运行使用 `wails3 dev`。首次构建会从固定版本源码编译已更新依赖的 Mihomo，后续构建复用经过校验的缓存。

TUN 权限隔离、本地规则文件路径要求及漏洞检查命令见[安全说明](docs/security.md)。

## 网络模式

**系统代理**会启动 mihomo 并更新 macOS 代理设置。它只影响遵循该设置的应用，不覆盖忽略系统代理的应用或 UDP 流量。断开连接时 Vela 会恢复原设置；GUI 异常退出时也会尝试恢复。


本地 mixed 代理默认监听 `127.0.0.1:7890`。侧边栏速度图显示系统网络接口流量；诊断页显示内核统计的代理流量。

如需移除已授权的 Tun 服务，先退出 Vela，再运行：

```sh
bin/vela --vela-tun-uninstall
```

## 检查

```sh
go test ./...
go vet ./...
npm --prefix frontend run typecheck
npm --prefix frontend test
```

前端功能划分和后端 Runner 的职责边界见[代码结构说明](docs/architecture.md)。

## 许可证

[MIT](LICENSE)
