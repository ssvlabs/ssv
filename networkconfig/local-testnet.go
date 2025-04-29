package networkconfig

import (
	"time"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

var LocalTestnet = NetworkConfig{
	Name: "local-testnet",
	BeaconConfig: BeaconConfig{
		BeaconName:                           string(spectypes.PraterNetwork),
		SlotDuration:                         spectypes.PraterNetwork.SlotDurationSec(),
		SlotsPerEpoch:                        phase0.Slot(spectypes.PraterNetwork.SlotsPerEpoch()),
		EpochsPerSyncCommitteePeriod:         256,
		SyncCommitteeSize:                    512,
		SyncCommitteeSubnetCount:             4,
		TargetAggregatorsPerSyncSubcommittee: 16,
		TargetAggregatorsPerCommittee:        16,
		IntervalsPerSlot:                     3,
		GenesisForkVersion:                   spectypes.PraterNetwork.ForkVersion(),
		GenesisTime:                          time.Unix(int64(spectypes.PraterNetwork.MinGenesisTime()), 0), // #nosec G115 -- time should not exceed int64
		GenesisValidatorsRoot:                phase0.Root(hexutil.MustDecode("0x043db0d9a83813551ee2f33450d23797757d430911a9320530ad8a0eabc43efb")),
		Forks: map[spec.DataVersion]phase0.Fork{
			spec.DataVersionPhase0: {
				Epoch:           phase0.Epoch(0),
				PreviousVersion: phase0.Version{0, 0, 0, 0},
				CurrentVersion:  phase0.Version{0, 0, 0, 0},
			},
			spec.DataVersionAltair: {
				Epoch:           phase0.Epoch(1),
				PreviousVersion: phase0.Version{0, 0, 0, 0},
				CurrentVersion:  phase0.Version{1, 0, 0, 0},
			},
			spec.DataVersionBellatrix: {
				Epoch:           phase0.Epoch(2),
				PreviousVersion: phase0.Version{1, 0, 0, 0},
				CurrentVersion:  phase0.Version{2, 0, 0, 0},
			},
			spec.DataVersionCapella: {
				Epoch:           phase0.Epoch(3),
				PreviousVersion: phase0.Version{2, 0, 0, 0},
				CurrentVersion:  phase0.Version{3, 0, 0, 0},
			},
			spec.DataVersionDeneb: {
				Epoch:           phase0.Epoch(4),
				PreviousVersion: phase0.Version{3, 0, 0, 0},
				CurrentVersion:  phase0.Version{4, 0, 0, 0},
			},
			spec.DataVersionElectra: {
				Epoch:           phase0.Epoch(5),
				PreviousVersion: phase0.Version{4, 0, 0, 0},
				CurrentVersion:  phase0.Version{5, 0, 0, 0},
			},
		},
	},
	SSVConfig: SSVConfig{
		DomainType:           spectypes.DomainType{0x0, 0x0, spectypes.JatoV2NetworkID.Byte(), 0x2},
		RegistryContractAddr: ethcommon.HexToAddress("0xC3CD9A0aE89Fff83b71b58b6512D43F8a41f363D"),
		Bootnodes: []string{
			"enr:-Li4QLR4Y1VbwiqFYKy6m-WFHRNDjhMDZ_qJwIABu2PY9BHjIYwCKpTvvkVmZhu43Q6zVA29sEUhtz10rQjDJkK3Hd-GAYiGrW2Bh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhCLdu_SJc2VjcDI1NmsxoQJTcI7GHPw-ZqIflPZYYDK_guurp_gsAFF5Erns3-PAvIN0Y3CCE4mDdWRwgg-h",
		},
		TotalEthereumValidators: TestNetwork.TotalEthereumValidators,
	},
}
