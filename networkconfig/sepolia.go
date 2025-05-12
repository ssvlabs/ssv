package networkconfig

import (
	"math/big"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

var Sepolia = NetworkConfig{
	Name: "sepolia",
	BeaconConfig: BeaconConfig{
		Beacon: beacon.NewNetwork(spectypes.SepoliaNetwork),
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
