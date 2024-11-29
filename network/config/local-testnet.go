package networkconfig

import (
	"math/big"
	"time"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ethcommon "github.com/ethereum/go-ethereum/common"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

var LocalTestnetSSV = SSV{
	Name:                      "local-testnet",
	GenesisDomainType:         spectypes.DomainType{0x0, 0x0, spectypes.JatoV2NetworkID.Byte(), 0x1},
	AlanDomainType:            spectypes.DomainType{0x0, 0x0, spectypes.JatoV2NetworkID.Byte(), 0x2},
	RegistrySyncOffset:        new(big.Int).SetInt64(1),
	RegistryContractAddr:      ethcommon.HexToAddress("0xC3CD9A0aE89Fff83b71b58b6512D43F8a41f363D"),
	DiscoveryProtocolID:       [6]byte{'s', 's', 'v', 'd', 'v', '5'},
	MaxValidatorsPerCommittee: HoleskySSV.MaxValidatorsPerCommittee,
	TotalEthereumValidators:   HoleskySSV.TotalEthereumValidators,
	Bootnodes: []string{
		"enr:-Li4QLR4Y1VbwiqFYKy6m-WFHRNDjhMDZ_qJwIABu2PY9BHjIYwCKpTvvkVmZhu43Q6zVA29sEUhtz10rQjDJkK3Hd-GAYiGrW2Bh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhCLdu_SJc2VjcDI1NmsxoQJTcI7GHPw-ZqIflPZYYDK_guurp_gsAFF5Erns3-PAvIN0Y3CCE4mDdWRwgg-h",
	},
}

var LocalTestnetBeaconConfig = Beacon{
	ConfigName:                           string(spectypes.HoleskyNetwork),
	CapellaForkVersion:                   phase0.Version{0x99, 0x99, 0x99, 0x99},
	SlotDuration:                         12 * time.Second,
	SlotsPerEpoch:                        32,
	EpochsPerSyncCommitteePeriod:         256,
	SyncCommitteeSize:                    512,
	SyncCommitteeSubnetCount:             4,
	TargetAggregatorsPerSyncSubcommittee: 16,
	TargetAggregatorsPerCommittee:        16,
	IntervalsPerSlot:                     3,
	Genesis: v1.Genesis{
		GenesisTime:           HoleskyBeaconConfig.Genesis.GenesisTime,
		GenesisValidatorsRoot: HoleskyBeaconConfig.Genesis.GenesisValidatorsRoot,
		GenesisForkVersion:    phase0.Version{0x99, 0x99, 0x99, 0x99},
	},
}
