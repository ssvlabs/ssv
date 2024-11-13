package networkconfig

import (
	"math/big"

	ethcommon "github.com/ethereum/go-ethereum/common"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

var TestingSSVConfig = SSV{
	Name:                 "testnet",
	GenesisDomainType:    spectypes.DomainType{0x0, 0x0, spectypes.JatoNetworkID.Byte(), 0x1},
	AlanDomainType:       spectypes.DomainType{0x0, 0x0, spectypes.JatoNetworkID.Byte(), 0x2},
	GenesisEpoch:         152834,
	RegistrySyncOffset:   new(big.Int).SetInt64(9015219),
	RegistryContractAddr: ethcommon.HexToAddress("0x4B133c68A084B8A88f72eDCd7944B69c8D545f03"),
	Bootnodes: []string{
		"enr:-Li4QFIQzamdvTxGJhvcXG_DFmCeyggSffDnllY5DiU47pd_K_1MRnSaJimWtfKJ-MD46jUX9TwgW5Jqe0t4pH41RYWGAYuFnlyth2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhCLdu_SJc2VjcDI1NmsxoQN4v-N9zFYwEqzGPBBX37q24QPFvAVUtokIo1fblIsmTIN0Y3CCE4uDdWRwgg-j",
	},
}

var TestingBeaconConfig = Beacon{
	GenesisForkVersionVal:           HoleskyBeaconConfig.GenesisForkVersionVal,
	MinGenesisTimeVal:               HoleskyBeaconConfig.MinGenesisTimeVal,
	SlotDurationVal:                 12,
	SlotsPerEpochVal:                32,
	EpochsPerSyncCommitteePeriodVal: 256,
	CapellaForkVersionVal:           HoleskyBeaconConfig.CapellaForkVersionVal,
}

var TestingNetworkConfig = NetworkConfig{
	SSV:    TestingSSVConfig,
	Beacon: TestingBeaconConfig,
}
