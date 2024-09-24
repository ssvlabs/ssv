package storage

import (
	"fmt"

	"golang.org/x/mod/semver"
)

type ConfigLock struct {
	NetworkName      string `json:"network_name"`
	UsingLocalEvents bool   `json:"using_local_events"`
	Version          string `json:"version"`
}

func (stored *ConfigLock) ValidateCompatibility(current *ConfigLock) error {
	if !semver.IsValid(stored.Version) {
		return fmt.Errorf("invalid stored version format: %s. The database must be removed or reinitialized", stored.Version)
	}
	if !semver.IsValid(current.Version) {
		return fmt.Errorf("invalid current version format: %s", current.Version)
	}

	storedMajor := semver.Major(stored.Version)
	currentMajor := semver.Major(current.Version)

	if semver.Compare(currentMajor, storedMajor) < 0 {
		return fmt.Errorf("downgrade detected. Current version %s (major: %s) is lower than stored version %s (major: %s)", current.Version, currentMajor, stored.Version, storedMajor)
	}

	if stored.NetworkName != current.NetworkName {
		return fmt.Errorf("network mismatch. Stored network %s does not match current network %s. The database must be removed or reinitialized", stored.NetworkName, current.NetworkName)
	}

	if stored.UsingLocalEvents && !current.UsingLocalEvents {
		return fmt.Errorf("disabling local events is not allowed. The database must be removed or reinitialized")
	}

	if !stored.UsingLocalEvents && current.UsingLocalEvents {
		return fmt.Errorf("enabling local events is not allowed. The database must be removed or reinitialized")
	}

	return nil
}
