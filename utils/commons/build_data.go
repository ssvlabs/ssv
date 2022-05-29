package commons

import "fmt"

var (
	appName = "SSV-Node"
	version = "latest"
)

// SetBuildData updates local vars for build data
func SetBuildData(app string, ver string) {
	appName = app
	version = ver
}

// GetBuildData returns build data
func GetBuildData() string {
	return fmt.Sprintf("%s:%s", appName, version)
}

// GetNodeVersion returns the current node version
func GetNodeVersion() string {
	return version
}
