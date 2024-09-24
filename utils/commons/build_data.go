package commons

import (
	"fmt"

	"golang.org/x/mod/semver"
)

var (
	appName = "SSV-Node"
	version = "v0.0.0-dev" // Default for local and untagged builds
)

// SetBuildData updates local vars for build data
func SetBuildData(app string, ver string) {
	appName = app
	version = normalizeVersion(ver)
}

// normalizeVersion ensures the version starts with "v" and is valid semver.
func normalizeVersion(ver string) string {
	if ver == "" {
		return version // Return the default version if no version provided
	}

	// If the version isn't valid semver, attempt to add the "v" prefix
	if !semver.IsValid(ver) {
		if semver.IsValid("v" + ver) {
			ver = "v" + ver
		} else {
			// Invalid version, fallback to default version
			fmt.Printf("Invalid version format: %s, defaulting to %s\n", ver, version)
			ver = version
		}
	}

	return ver
}

// GetBuildData returns build data as "AppName:Version"
func GetBuildData() string {
	return fmt.Sprintf("%s:%s", appName, version)
}

// GetNodeVersion returns the current node version
func GetNodeVersion() string {
	return version
}
