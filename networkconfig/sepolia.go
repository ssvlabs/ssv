package networkconfig

import (
	"math/big"

	ethcommon "github.com/ethereum/go-ethereum/common"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

const SepoliaName = "sepolia"

var SepoliaSSV = &SSVConfig{
	DomainType:           spectypes.DomainType{0x0, 0x0, 0x5, 0x69},
	RegistrySyncOffset:   new(big.Int).SetInt64(7795814),
	RegistryContractAddr: ethcommon.HexToAddress("0x261419B48F36EdF420743E9f91bABF4856e76f99"),
	DiscoveryProtocolID:  [6]byte{'s', 's', 'v', 'd', 'v', '5'},
	Bootnodes: []string{
		// SSV Labs
		"enr:-Ja4QIE0Ml0a8Pq9zD-0g9KYGN3jAMPJ0CAP0i16fK-PSHfLeORl-Z5p8odoP1oS5S2E8IsF5jNG7gqTKhjVsHR-Z_CGAZXrnTJrgmlkgnY0gmlwhCOjXGWJc2VjcDI1NmsxoQKCRDQsIdFsJDmu_ZU2H6b2_HRJbuUneDXHLfFkSQH9O4Nzc3YBg3RjcIITioN1ZHCCD6I",
	},
	MaxF:                    4,
	TotalEthereumValidators: 1781, // active_validators from https://sepolia.beaconcha.in/index/data on Mar 20, 2025
	GasLimit36Epoch:         0,
}
