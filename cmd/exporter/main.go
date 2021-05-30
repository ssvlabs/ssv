package main

import "github.com/bloxapp/ssv/cli/exporter"

var (
	// AppName is the application name
	AppName = "exporter-Node"

	// Version is the app version
	Version = "latest"
)

func main() {
	exporter.Execute(AppName, Version)
}
