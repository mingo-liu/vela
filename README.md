<p align="center">
  <img src="build/appicon.png" alt="Vela logo" width="200">
</p>

<h1 align="center">Vela</h1>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-green" alt="MIT license"></a>
  <a href="https://github.com/mingo-liu/vela/actions/workflows/test.yml"><img src="https://github.com/mingo-liu/vela/actions/workflows/test.yml/badge.svg" alt="Test status"></a>
  <img src="https://img.shields.io/badge/macOS-Apple_Silicon-black?logo=apple" alt="macOS Apple Silicon">
  <img src="https://img.shields.io/badge/Go-1.26.8-00ADD8?logo=go&amp;logoColor=white" alt="Go 1.26.8">
  <img src="https://img.shields.io/badge/Wails-v3-blue" alt="Wails v3">
  <img src="https://img.shields.io/badge/React-18-149ECA?logo=react&amp;logoColor=white" alt="React 18">
</p>

<p align="center">
  <a href="README.md">English</a> · <a href="README.zh-CN.md">简体中文</a>
</p>

Vela is a macOS proxy client built with Wails v3, React, and mihomo. It provides a desktop interface for managing a mihomo configuration and routing traffic through the macOS system proxy or Tun mode.

## Features

- Import a local mihomo YAML file or an HTTP/HTTPS subscription; edit, remove, and automatically refresh subscriptions, and switch the active configuration.
- Use proxy providers, rule providers, Sniffer, GeoIP, and GeoSite rules in profiles. Local proxy-provider files must be under `providers/` or `proxies/` in Vela's data directory.
- Tun mode supports inline and local proxy providers; HTTP proxy providers are available in system proxy mode.
- Choose nodes in manual proxy groups, including provider nodes, measure latency, and sort by name or latency. Valid node choices survive subscription updates.
- View task progress and cancel subscription downloads or latency tests; disconnect without waiting for probes.
- Switch between Rule, Global, and Direct routing modes.
- Connect through the system proxy or Tun mode. These modes are mutually exclusive.
- Read state and logs during connection startup, cancel startup, and cancel pending startup when quitting the app.
- Configure startup behavior, local proxy port, log level, and interface language. Closing the window keeps Vela in the menu bar; quitting stops the core.
- Inspect active connections, proxy traffic, proxy chains, and loaded rules in Diagnostics.
- Use Edit configuration on a subscription card to inspect original rules and add exact-domain or domain-suffix rules for Direct, Block, or an existing proxy group. Edit, reorder, enable, and delete custom rules independently for each subscription. Rules take priority and survive updates; switching subscriptions applies their own rules in Rule mode. Editing an inactive subscription does not switch the current profile. Missing group destinations remain saved but inactive. The active local profile also has a rule editor.
- Pause periodic interface refreshes while the window is hidden or minimised, and refresh immediately when shown. Proxy connections keep running.

## Build and run

Requires macOS on Apple Silicon, Go 1.26.8+, Node.js 24, npm, Xcode Command Line Tools, and Wails.

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.24
wails3 package
open bin/Vela.app
```

For development, run `wails3 dev`. The first build compiles the pinned Mihomo source with updated dependencies; subsequent builds reuse a verified cache.

See [security boundaries and verification](docs/security.md) for TUN isolation, local rule-file paths and vulnerability checks.

## Network modes

**System proxy** starts mihomo and updates the macOS proxy settings. It affects apps that follow those settings; it does not cover apps that ignore them or UDP traffic. Vela restores the previous settings when disconnected and attempts to restore them if the GUI exits unexpectedly.

The local mixed proxy listens on `127.0.0.1:7890` by default.
The sidebar speed chart shows system interface traffic; Diagnostics shows proxy traffic reported by the core.

To remove the authorized Tun service, quit Vela and run:

```sh
bin/vela --vela-tun-uninstall
```

## Checks

```sh
go test ./...
go vet ./...
npm --prefix frontend run typecheck
npm --prefix frontend test
```

See [code organization](docs/architecture.md) for frontend feature boundaries and
backend Runner responsibilities.

## License

[MIT](LICENSE)
