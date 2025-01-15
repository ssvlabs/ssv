package networkconfig

import (
	"math/big"
	"time"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ethcommon "github.com/ethereum/go-ethereum/common"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

var HoleskySSV = SSV{
	Name:                    "holesky",
	DomainType:              spectypes.DomainType{0x0, 0x0, 0x5, 0x2},
	RegistrySyncOffset:      new(big.Int).SetInt64(181612),
	RegistryContractAddr:    ethcommon.HexToAddress("0x38A4794cCEd47d3baf7370CcC43B560D3a1beEFA"),
	DiscoveryProtocolID:     [6]byte{'s', 's', 'v', 'd', 'v', '5'},
	TotalEthereumValidators: 1757795, // active_validators from https://holesky.beaconcha.in/index/data on Nov 20, 2024
	Bootnodes: []string{
		// SSV Labs
		"enr:-Ja4QKFD3u5tZob7xukp-JKX9QJMFqqI68cItsE4tBbhsOyDR0M_1UUjb35hbrqvTP3bnXO_LnKh-jNLTeaUqN4xiduGAZKaP_sagmlkgnY0gmlwhDb0fh6Jc2VjcDI1NmsxoQMw_H2anuiqP9NmEaZwbUfdvPFog7PvcKmoVByDa576SINzc3YBg3RjcIITioN1ZHCCD6I",
		"enr:-Li4QFIQzamdvTxGJhvcXG_DFmCeyggSffDnllY5DiU47pd_K_1MRnSaJimWtfKJ-MD46jUX9TwgW5Jqe0t4pH41RYWGAYuFnlyth2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhCLdu_SJc2VjcDI1NmsxoQN4v-N9zFYwEqzGPBBX37q24QPFvAVUtokIo1fblIsmTIN0Y3CCE4uDdWRwgg-j",
	},
}

var HoleskyBeaconConfig = Beacon{
	ConfigName:                           string(spectypes.HoleskyNetwork),
	CapellaForkVersion:                   phase0.Version{0x04, 0x01, 0x70, 0x00},
	SlotDuration:                         12 * time.Second,
	SlotsPerEpoch:                        32,
	EpochsPerSyncCommitteePeriod:         256,
	SyncCommitteeSize:                    512,
	SyncCommitteeSubnetCount:             4,
	TargetAggregatorsPerSyncSubcommittee: 16,
	TargetAggregatorsPerCommittee:        16,
	IntervalsPerSlot:                     3,
	Genesis: v1.Genesis{
		GenesisTime:           time.Unix(1695902400, 0),
		GenesisValidatorsRoot: phase0.Root{0xD8, 0xEA, 0x17, 0x1F, 0x3C, 0x94, 0xAE, 0xA2, 0x1E, 0xBC, 0x42, 0xA1, 0xED, 0x61, 0x05, 0x2A, 0xCF, 0x3F, 0x92, 0x09, 0xC0, 0x0E, 0x4E, 0xFB, 0xAA, 0xDD, 0xAC, 0x09, 0xED, 0x9B, 0x80, 0x78},
		GenesisForkVersion:    phase0.Version{0x01, 0x01, 0x70, 0x00},
	},
}
