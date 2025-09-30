package networkconfig

import (
	"math/big"

	ethcommon "github.com/ethereum/go-ethereum/common"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

var FusakaDevnet3SSV = &SSV{
	Name:                 "fusaka-devnet3-stage",
	DomainType:           spectypes.DomainType{0x0, 0x0, 0x32, 0x29},
	RegistrySyncOffset:   new(big.Int).SetInt64(396240),
	RegistryContractAddr: ethcommon.HexToAddress("0xad45A78180961079BFaeEe349704F411dfF947C6"),
	DiscoveryProtocolID:  [6]byte{'s', 's', 'v', 'd', 'v', '5'},
	Bootnodes: []string{
		// SSV Labs
		"enr:-Ja4QGIbNK5LuceDavwE2wRbngOBcXMOKoPHY-tLGeYzJk6TWD7Xb66apMDTyVRNZJv-odvScdILwdZpvwbeKOn_85yGAZl1aDFMgmlkgnY0gmlwhErcHL-Jc2VjcDI1NmsxoQP_bBE-ZYvaXKBR3dRYMN5K_lZP-q-YsBzDZEtxH_4T_YNzc3YBg3RjcIITioN1ZHCCD6I",
	},
//	TotalEthereumValidators: 1781, // active_validators from https://sepolia.beaconcha.in/index/data on Mar 20, 2025
	Forks: SSVForks{
		Alan:       0,
		GasLimit36: 0,
	},
}
