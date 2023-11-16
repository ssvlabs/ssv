package main

import (
	"fmt"
	"github.com/bloxapp/ssv/cli"
)

// AppName is the application name
var AppName = "SSV-Node"

// Version is the app version
var Version = "latest"

// Branch is the git branch this version was built on
var Branch = "main"

// Commit is the git commit this version was built on
var Commit = "unknown"

func main() {
	version := fmt.Sprintf("%s-%s-%s", Version, Branch, Commit)
	cli.Execute(AppName, version)
}
