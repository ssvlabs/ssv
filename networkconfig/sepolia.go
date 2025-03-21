package networkconfig

import (
	"math/big"

	ethcommon "github.com/ethereum/go-ethereum/common"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

var SepoliaSSV = SSV{
	Name:                    "sepolia",
	DomainType:              spectypes.DomainType{0x0, 0x0, 0x5, 0x69},
	RegistrySyncOffset:      new(big.Int).SetInt64(7795814),
	RegistryContractAddr:    ethcommon.HexToAddress("0x261419B48F36EdF420743E9f91bABF4856e76f99"),
	DiscoveryProtocolID:     [6]byte{'s', 's', 'v', 'd', 'v', '5'},
	TotalEthereumValidators: 1781, // active_validators from https://sepolia.beaconcha.in/index/data on Mar 20, 2025
	Bootnodes: []string{
		// SSV Labs
		"enr:-Ja4QKO4MHCCK34qIKn-ZaAcWcwuJug2PUQHG2F71KRZyFFuT20vWIGwz0YrvoUjpdx9Fz7Qt9YRllgcR_rEcHtu-7KGAZVCasuFgmlkgnY0gmlwhAorJeOJc2VjcDI1NmsxoQP_bBE-ZYvaXKBR3dRYMN5K_lZP-q-YsBzDZEtxH_4T_YNzc3YBg3RjcIITioN1ZHCCD6I",
	},
}
