package networkconfig

import (
	"math/big"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ethcommon "github.com/ethereum/go-ethereum/common"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

var LocalTestnet = NetworkConfig{
	Name:                 "local-testnet",
	BeaconConfig:         LocalTestnetBeaconConfig,
	GenesisDomainType:    spectypes.DomainType{0x0, 0x0, spectypes.JatoV2NetworkID.Byte(), 0x1},
	AlanDomainType:       spectypes.DomainType{0x0, 0x0, spectypes.JatoV2NetworkID.Byte(), 0x2},
	GenesisEpoch:         1,
	RegistrySyncOffset:   new(big.Int).SetInt64(1),
	RegistryContractAddr: ethcommon.HexToAddress("0xC3CD9A0aE89Fff83b71b58b6512D43F8a41f363D"),
	Bootnodes: []string{
		"enr:-Li4QLR4Y1VbwiqFYKy6m-WFHRNDjhMDZ_qJwIABu2PY9BHjIYwCKpTvvkVmZhu43Q6zVA29sEUhtz10rQjDJkK3Hd-GAYiGrW2Bh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhCLdu_SJc2VjcDI1NmsxoQJTcI7GHPw-ZqIflPZYYDK_guurp_gsAFF5Erns3-PAvIN0Y3CCE4mDdWRwgg-h",
	},
}

var LocalTestnetBeaconConfig = BeaconConfig{
	GenesisForkVersionVal:           phase0.Version{0x99, 0x99, 0x99, 0x99},
	MinGenesisTimeVal:               HoleskyBeaconConfig.MinGenesisTimeVal,
	SlotDurationVal:                 12,
	SlotsPerEpochVal:                32,
	EpochsPerSyncCommitteePeriodVal: 256,
	CapellaForkVersionVal:           phase0.Version{0x99, 0x99, 0x99, 0x99},
}
