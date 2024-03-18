package main

import (
	"fmt"
	"github.com/bloxapp/ssv/cli/tests"
)

// AppName is the application name
var AppName = "SSV-Node-tests"

// Version is the app version
var Version = "latest"

// Commit is the git commit this version was built on
var Commit = "unknown"

func main() {
	version := fmt.Sprintf("%s-%s", Version, Commit)
	tests.Execute(AppName, version)
}
