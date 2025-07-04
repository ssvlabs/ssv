package networkconfig

import (
	"fmt"
)

//go:generate go tool -modfile=../tool.mod mockgen -package=networkconfig -destination=./network_mock.go -source=./network.go

// NetworkType returns network type which is a combination of some specific Ethereum network (mainnet, hoodi, etc.) +
// SSV-specific version (alan, etc.).
func NetworkType(networkName string) string {
	const forkName = "alan"
	return fmt.Sprintf("%s:%s", networkName, forkName)
}

// Network represents aggregate network configuration combining Ethereum-specific and SSV-specific settings all in
// one place.
type Network interface {
	Beacon
	SSV
}

// NetworkConfig implements Network.
type NetworkConfig struct {
	*BeaconConfig
	*SSVConfig
}
