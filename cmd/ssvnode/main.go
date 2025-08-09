package main

import (
	"fmt"

	"github.com/ssvlabs/ssv/cli"
)

// AppName is the application name
var AppName = "SSV-Node-test-ci"

// Version is the app version
var Version = "latest"

// Commit is the git commit this version was built on
var Commit = "unknown"

func main() {
	version := fmt.Sprintf("%s-%s", Version, Commit)
	cli.Execute(AppName, version)
}
