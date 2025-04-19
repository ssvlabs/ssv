package networkconfig

import (
	"math/big"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

var Sepolia = NetworkConfig{
	Name: "sepolia",
	BeaconConfig: BeaconConfig{
		BeaconName:    string(spectypes.SepoliaNetwork),
		SlotDuration:  spectypes.SepoliaNetwork.SlotDurationSec(),
		SlotsPerEpoch: phase0.Slot(spectypes.SepoliaNetwork.SlotsPerEpoch()),
		ForkVersion:   spectypes.SepoliaNetwork.ForkVersion(),
		GenesisTime:   time.Unix(int64(spectypes.SepoliaNetwork.MinGenesisTime()), 0), // #nosec G115 -- time should not exceed int64
	},
	SSVConfig: SSVConfig{
		DomainType:           spectypes.DomainType{0x0, 0x0, 0x5, 0x69},
		RegistrySyncOffset:   new(big.Int).SetInt64(7795814),
		RegistryContractAddr: "0x261419B48F36EdF420743E9f91bABF4856e76f99",
		DiscoveryProtocolID:  [6]byte{'s', 's', 'v', 'd', 'v', '5'},
		Bootnodes: []string{
			// SSV Labs
			"enr:-Ja4QIE0Ml0a8Pq9zD-0g9KYGN3jAMPJ0CAP0i16fK-PSHfLeORl-Z5p8odoP1oS5S2E8IsF5jNG7gqTKhjVsHR-Z_CGAZXrnTJrgmlkgnY0gmlwhCOjXGWJc2VjcDI1NmsxoQKCRDQsIdFsJDmu_ZU2H6b2_HRJbuUneDXHLfFkSQH9O4Nzc3YBg3RjcIITioN1ZHCCD6I",
		},
	},
}
