package main

import (
	"log"

	"github.com/mingo-liu/vela/internal/platform/macos"
)

func main() {
	if err := macos.RunTunService(); err != nil {
		log.Fatal(err)
	}
}
