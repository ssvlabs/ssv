package storage

import (
	"fmt"
)

type ConfigLock struct {
	NetworkName      string `json:"network_name"`
	UsingLocalEvents bool   `json:"using_local_events"`
	UsingSSVSigner   bool   `json:"using_ssv_signer"`
}

func (stored *ConfigLock) ValidateCompatibility(current *ConfigLock) error {
	if stored.NetworkName != current.NetworkName {
		return fmt.Errorf("network mismatch. Stored network %s does not match current network %s. The database must be removed or reinitialized", stored.NetworkName, current.NetworkName)
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
