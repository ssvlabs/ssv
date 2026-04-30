package runner

import (
	"maps"

	"github.com/ssvlabs/ssv/networkconfig"
)

// cloneTestNetworkConfig returns a deep-enough copy of TestNetwork for
// tests that need to mutate beacon-config timing fields without
// stepping on each other. The non-Beacon fields stay shared (the tests
// don't mutate them).
func cloneTestNetworkConfig() *networkconfig.Network {
	cfg := *networkconfig.TestNetwork
	beaconCfg := *networkconfig.TestNetwork.Beacon
	if beaconCfg.Forks != nil {
		beaconCfg.Forks = maps.Clone(beaconCfg.Forks)
	}
	cfg.Beacon = &beaconCfg
	return &cfg
}
