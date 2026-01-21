package spectest

import (
	"math"

	eth2clientspec "github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"golang.org/x/exp/maps"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/ssvlabs/ssv/networkconfig"
)

func keySetFromShares(shares map[phase0.ValidatorIndex]*spectypes.Share) *spectestingutils.TestKeySet {
	for _, share := range shares {
		return spectestingutils.KeySetForShare(share)
	}
	return nil
}

func mapForKeys(m map[string]any, keys ...string) map[string]any {
	for _, key := range keys {
		if value, ok := m[key]; ok {
			if cast, ok := value.(map[string]any); ok {
				return cast
			}
		}
	}
	return nil
}

func testNetworkConfig(needsAggregator bool) *networkconfig.Network {
	if !needsAggregator {
		return networkconfig.TestNetwork
	}

	beaconCfg := *networkconfig.TestNetwork.Beacon
	beaconCfg.Forks = maps.Clone(beaconCfg.Forks)
	fuluFork := beaconCfg.Forks[eth2clientspec.DataVersionFulu]
	fuluFork.Epoch = math.MaxUint64
	beaconCfg.Forks[eth2clientspec.DataVersionFulu] = fuluFork

	netCfg := *networkconfig.TestNetwork
	netCfg.Beacon = &beaconCfg
	return &netCfg
}
