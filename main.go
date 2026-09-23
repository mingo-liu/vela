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
