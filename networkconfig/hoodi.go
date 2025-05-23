package networkconfig

import (
	"math/big"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

var Hoodi = NetworkConfig{
	Name: "hoodi",
	BeaconConfig: BeaconConfig{
		BeaconName:                           string(spectypes.HoodiNetwork),
		SlotDuration:                         spectypes.HoodiNetwork.SlotDurationSec(),
		SlotsPerEpoch:                        spectypes.HoodiNetwork.SlotsPerEpoch(),
		EpochsPerSyncCommitteePeriod:         256,
		SyncCommitteeSize:                    512,
		SyncCommitteeSubnetCount:             4,
		TargetAggregatorsPerSyncSubcommittee: 16,
		TargetAggregatorsPerCommittee:        16,
		IntervalsPerSlot:                     3,
		ForkVersion:                          spectypes.HoodiNetwork.ForkVersion(),
		GenesisTime:                          time.Unix(int64(spectypes.HoodiNetwork.MinGenesisTime()), 0), // #nosec G115 -- time should not exceed int64
		GenesisValidatorsRoot:                phase0.Root(hexutil.MustDecode("0x212f13fc4df078b6cb7db228f1c8307566dcecf900867401a92023d7ba99cb5f")),
	},
	SSVConfig: SSVConfig{
		DomainType:           spectypes.DomainType{0x0, 0x0, 0x5, 0x3},
		RegistrySyncOffset:   new(big.Int).SetInt64(1065),
		RegistryContractAddr: ethcommon.HexToAddress("0x58410Bef803ECd7E63B23664C586A6DB72DAf59c"),
		DiscoveryProtocolID:  [6]byte{'s', 's', 'v', 'd', 'v', '5'},
		Bootnodes: []string{
			// SSV Labs
			"enr:-Ja4QIKlyNFuFtTOnVoavqwmpgSJXfhSmhpdSDOUhf5-FBr7bBxQRvG6VrpUvlkr8MtpNNuMAkM33AseduSaOhd9IeWGAZWjRbnvgmlkgnY0gmlwhCNVVTCJc2VjcDI1NmsxoQNTTyiJPoZh502xOZpHSHAfR-94NaXLvi5J4CNHMh2tjoNzc3YBg3RjcIITioN1ZHCCD6I",
		},
		TotalEthereumValidators: 1107955, // active_validators from https://hoodi.beaconcha.in/index/data on Apr 18, 2025
	},
}
