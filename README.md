# Vela

[English](README.md) · [简体中文](README.zh-CN.md)

Vela is a macOS proxy client built with Wails v3, React, and mihomo. It provides a desktop interface for managing a mihomo configuration and routing traffic through the macOS system proxy or Tun mode.

## Features

- Import a local mihomo YAML file or an HTTP/HTTPS subscription; refresh subscriptions manually and switch the active configuration.
- Choose nodes in manual proxy groups, measure latency, and sort nodes by name or latency.
- Switch between Rule, Global, and Direct routing modes.
- Connect through the system proxy or Tun mode. These modes are mutually exclusive.
- Configure startup behavior, local proxy port, log level, and interface language. Closing the window keeps Vela in the menu bar; quitting stops the core.

## Build and run

Requires macOS on Apple Silicon, Go 1.25+, Node.js 24, npm, Xcode Command Line Tools, and Wails.

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.24
wails3 package
open bin/Vela.app
```

For development, run `wails3 dev`.

## Network modes

**System proxy** starts mihomo and updates the macOS proxy settings. It affects apps that follow those settings; it does not cover apps that ignore them or UDP traffic. Vela restores the previous settings when disconnected and attempts to restore them if the GUI exits unexpectedly.

The local mixed proxy listens on `127.0.0.1:7890` by default.

To remove the authorized Tun service, quit Vela and run:

```sh
bin/vela --vela-tun-uninstall
```

## Checks

```sh
go test ./...
go vet ./...
npm --prefix frontend run typecheck
```

## License

[MIT](LICENSE)
