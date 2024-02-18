package networkconfig

import (
	"math/big"

	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
)

var Holesky = NetworkConfig{
	Name:                 "holesky",
	Beacon:               beacon.NewNetwork(spectypes.HoleskyNetwork),
	Domain:               spectypes.DomainType{0x0, 0x0, 0x5, 0x1},
	GenesisEpoch:         1,
	RegistrySyncOffset:   new(big.Int).SetInt64(181612),
	RegistryContractAddr: "0x38A4794cCEd47d3baf7370CcC43B560D3a1beEFA",
	Bootnodes: []string{
		"enr:-Li4QFIQzamdvTxGJhvcXG_DFmCeyggSffDnllY5DiU47pd_K_1MRnSaJimWtfKJ-MD46jUX9TwgW5Jqe0t4pH41RYWGAYuFnlyth2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhCLdu_SJc2VjcDI1NmsxoQN4v-N9zFYwEqzGPBBX37q24QPFvAVUtokIo1fblIsmTIN0Y3CCE4uDdWRwgg-j",
	},
	WhitelistedOperatorKeys:       []string{},
	PermissionlessActivationEpoch: 13950, // Nov-29-2023 12:00:00 PM UTC
}
