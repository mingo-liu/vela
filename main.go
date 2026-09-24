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
	if os.Geteuid() != os.Getuid() {
		log.Fatal("Vela 图形程序不能以提权身份运行")
	}
	if len(os.Args) > 1 && os.Args[1] == "--vela-tun-client" {
		if err := macos.RunTunClient(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "--vela-tun-uninstall" {
		if err := macos.UninstallTunService(); err != nil {
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
