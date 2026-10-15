package networkconfig

import (
	"maps"
	"math/big"
	"time"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// friendsGenesis is the fixed clock origin of friends networks. There is no beacon chain behind
// it: the slot clock only paces p2p internals (topic scoring, rate limits).
var friendsGenesis = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

// FriendsNetwork returns the network config of a friends-mode (dKMS) network: SSV's p2p and
// consensus stack with a synthetic beacon config, since no beacon node is involved. Friends
// networks start post-Boole (committee topics keyed by operator set) and use their own domain so
// they never mix with staking networks.
func FriendsNetwork(name string) *Network {
	beacon := *TestNetwork.Beacon
	beacon.Name = "friends"
	beacon.GenesisTime = friendsGenesis
	beacon.Forks = maps.Clone(TestNetwork.Beacon.Forks)
	for v, f := range beacon.Forks {
		f.Epoch = 0
		beacon.Forks[v] = f
	}
	beacon.Forks[spec.DataVersionPhase0] = phase0.Fork{}

	return &Network{
		Beacon: &beacon,
		SSV: &SSV{
			Name:                    name,
			DomainType:              spectypes.DomainType{'F', 'R', 'N', 0x1},
			NextDomainType:          spectypes.DomainType{'F', 'R', 'N', 0x2},
			RegistrySyncOffset:      new(big.Int),
			DiscoveryProtocolID:     [6]byte{'s', 's', 'v', 'd', 'v', '5'},
			TotalEthereumValidators: TestNetwork.TotalEthereumValidators,
			Forks:                   SSVForks{Boole: 0},
		},
	}
}
