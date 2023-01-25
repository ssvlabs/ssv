package main

import (
	"github.com/bloxapp/ssv/cli"
	"log"
)

var (
	// AppName is the application name
	AppName = "SSV-Node"

	// Version is the app version
	Version = "latest"
)

func main() {
	log.Println("app version:", Version)
	cli.Execute(AppName, Version)
}
