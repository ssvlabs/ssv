package storage

import (
	"fmt"
)

type ConfigLock struct {
	// NetworkType has `network_name` annotation (and not `network_type`) for backward-compatibility with existing DBs.
	NetworkType      string `json:"network_name"`
	UsingLocalEvents bool   `json:"using_local_events"`
	UsingSSVSigner   bool   `json:"using_ssv_signer"`
}

func (stored *ConfigLock) ValidateCompatibility(current *ConfigLock) error {
	if stored.NetworkType != current.NetworkType {
		return fmt.Errorf("network mismatch. Stored network %s does not match current network %s. The database must be removed or reinitialized", stored.NetworkType, current.NetworkType)
	}

	if stored.UsingLocalEvents && !current.UsingLocalEvents {
		return fmt.Errorf("disabling local events is not allowed. The database must be removed or reinitialized")
	}

	if !stored.UsingLocalEvents && current.UsingLocalEvents {
		return fmt.Errorf("enabling local events is not allowed. The database must be removed or reinitialized")
	}

	if stored.UsingSSVSigner && !current.UsingSSVSigner {
		return fmt.Errorf("disabling ssv-signer is not allowed. The database must be removed or reinitialized")
	}

	if !stored.UsingSSVSigner && current.UsingSSVSigner {
		return fmt.Errorf("enabling ssv-signer is not allowed. The database must be removed or reinitialized")
	}

	return nil
}
