package networkconfig

import (
	"math/big"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ethcommon "github.com/ethereum/go-ethereum/common"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

var TestingSSVConfig = SSV{
	Name:                 "testnet",
	GenesisDomainType:    spectypes.DomainType{0x0, 0x0, spectypes.JatoNetworkID.Byte(), 0x1},
	AlanDomainType:       spectypes.DomainType{0x0, 0x0, spectypes.JatoNetworkID.Byte(), 0x2},
	RegistrySyncOffset:   new(big.Int).SetInt64(9015219),
	RegistryContractAddr: ethcommon.HexToAddress("0x4B133c68A084B8A88f72eDCd7944B69c8D545f03"),
	Bootnodes: []string{
		"enr:-Li4QFIQzamdvTxGJhvcXG_DFmCeyggSffDnllY5DiU47pd_K_1MRnSaJimWtfKJ-MD46jUX9TwgW5Jqe0t4pH41RYWGAYuFnlyth2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhCLdu_SJc2VjcDI1NmsxoQN4v-N9zFYwEqzGPBBX37q24QPFvAVUtokIo1fblIsmTIN0Y3CCE4uDdWRwgg-j",
	},
}

var TestingBeaconConfig = Beacon{
	ConfigName:                           string(spectypes.BeaconTestNetwork),
	GenesisForkVersion:                   phase0.Version{0x99, 0x99, 0x99, 0x99},
	CapellaForkVersion:                   phase0.Version{0x99, 0x99, 0x99, 0x99},
	MinGenesisTime:                       time.Unix(1616508000, 0),
	GenesisDelay:                         0,
	SlotDuration:                         12 * time.Second,
	SlotsPerEpoch:                        32,
	EpochsPerSyncCommitteePeriod:         256,
	SyncCommitteeSize:                    512,
	SyncCommitteeSubnetCount:             4,
	TargetAggregatorsPerSyncSubcommittee: 16,
	TargetAggregatorsPerCommittee:        16,
	IntervalsPerSlot:                     3,
}

var TestingNetworkConfig = NetworkConfig{
	SSV:    TestingSSVConfig,
	Beacon: TestingBeaconConfig,
}
