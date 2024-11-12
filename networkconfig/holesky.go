package networkconfig

import (
	"math/big"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ethcommon "github.com/ethereum/go-ethereum/common"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

var Holesky = NetworkConfig{
	Name:                 "holesky",
	BeaconConfig:         HoleskyBeaconConfig,
	GenesisDomainType:    spectypes.DomainType{0x0, 0x0, 0x5, 0x1},
	AlanDomainType:       spectypes.DomainType{0x0, 0x0, 0x5, 0x2},
	GenesisEpoch:         1,
	AlanForkEpoch:        84600, // Oct-08-2024 12:00:00 PM UTC
	RegistrySyncOffset:   new(big.Int).SetInt64(181612),
	RegistryContractAddr: ethcommon.HexToAddress("0x38A4794cCEd47d3baf7370CcC43B560D3a1beEFA"),
	DiscoveryProtocolID:  [6]byte{'s', 's', 'v', 'd', 'v', '5'},
	Bootnodes: []string{
		"enr:-Li4QFIQzamdvTxGJhvcXG_DFmCeyggSffDnllY5DiU47pd_K_1MRnSaJimWtfKJ-MD46jUX9TwgW5Jqe0t4pH41RYWGAYuFnlyth2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhCLdu_SJc2VjcDI1NmsxoQN4v-N9zFYwEqzGPBBX37q24QPFvAVUtokIo1fblIsmTIN0Y3CCE4uDdWRwgg-j",
	},
}

var HoleskyBeaconConfig = BeaconConfig{
	GenesisForkVersionVal:           phase0.Version{0x01, 0x01, 0x70, 0x00},
	MinGenesisTimeVal:               time.Unix(1695902400, 0),
	SlotDurationVal:                 12 * time.Second,
	SlotsPerEpochVal:                32,
	EpochsPerSyncCommitteePeriodVal: 256,
	CapellaForkVersionVal:           phase0.Version{0x04, 0x01, 0x70, 0x00},
}
