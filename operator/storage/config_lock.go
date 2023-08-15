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
		return fmt.Errorf("can't change network from %q to %q in an existing database, it must be removed first",
			stored.NetworkName, current.NetworkName)
	}

	if stored.UsingLocalEvents && !current.UsingLocalEvents {
		return fmt.Errorf("can't switch off localevents, database must be removed first")
	}

	if !stored.UsingLocalEvents && current.UsingLocalEvents {
		return fmt.Errorf("can't switch on localevents, database must be removed first")
	}

	return nil
}
