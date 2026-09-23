package main

import (
	"embed"
	"log"
	"os"

	"github.com/mingo-liu/vela/internal/app"
	"github.com/mingo-liu/vela/internal/platform/macos"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if os.Geteuid() != os.Getuid() && (len(os.Args) < 2 || os.Args[1] != "--vela-tun-helper") {
		log.Fatal("Tun 辅助程序只能用于启动 Tun 内核")
	}
	if len(os.Args) > 1 && os.Args[1] == "--vela-tun-helper" {
		if err := macos.RunTunHelper(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "--vela-tun-uninstall" {
		if err := macos.UninstallTunHelper(); err != nil {
			log.Fatal(err)
		}
		return
	}
	if len(os.Args) == 3 && os.Args[1] == "--vela-proxy-watch" {
		if err := macos.WatchSystemProxy(os.Args[2]); err != nil {
			log.Fatal(err)
		}
		return
	}
	if err := app.Run(assets); err != nil {
		log.Fatal(err)
	}
}
