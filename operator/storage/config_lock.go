package storage

import (
	"fmt"
)

type ConfigLock struct {
	NetworkName      string `json:"network_name"`
	UsingLocalEvents bool   `json:"using_local_events"`
}

func (stored *ConfigLock) EnsureSameWith(current *ConfigLock) error {
	if stored.NetworkName != current.NetworkName {
		return fmt.Errorf("node was already run with a different network name, the database needs to be cleaned to switch the network, current %q, stored %q",
			current.NetworkName, stored.NetworkName)
	}

	if stored.UsingLocalEvents && !current.UsingLocalEvents {
		return fmt.Errorf("node was already run with local events, the database needs to be cleaned to use real events")
	}

	if !stored.UsingLocalEvents && current.UsingLocalEvents {
		return fmt.Errorf("node was already run with real events, the database needs to be cleaned to use local events")
	}

	return nil
}
