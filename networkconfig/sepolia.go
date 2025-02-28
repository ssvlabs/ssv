package networkconfig

import (
	"math/big"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

var Sepolia = NetworkConfig{
	Name:                 "sepolia",
	Beacon:               beacon.NewNetwork(spectypes.SepoliaNetwork),
	DomainType:           spectypes.DomainType{0x0, 0x0, 0x5, 0x69},
	GenesisEpoch:         1,
	RegistrySyncOffset:   new(big.Int).SetInt64(7795814),
	RegistryContractAddr: "0x261419B48F36EdF420743E9f91bABF4856e76f99",
	DiscoveryProtocolID:  [6]byte{'s', 's', 'v', 'd', 'v', '5'},
	Bootnodes: []string{
		// SSV Labs
		"enr:-Ja4QKO4MHCCK34qIKn-ZaAcWcwuJug2PUQHG2F71KRZyFFuT20vWIGwz0YrvoUjpdx9Fz7Qt9YRllgcR_rEcHtu-7KGAZVCasuFgmlkgnY0gmlwhAorJeOJc2VjcDI1NmsxoQP_bBE-ZYvaXKBR3dRYMN5K_lZP-q-YsBzDZEtxH_4T_YNzc3YBg3RjcIITioN1ZHCCD6I",
	},
}
