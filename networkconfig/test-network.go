package networkconfig

import (
	"math/big"
	"time"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

var TestNetwork = &Network{
	Beacon: &Beacon{
		Name:                                 string(spectypes.BeaconTestNetwork),
		SlotDuration:                         spectypes.BeaconTestNetwork.SlotDurationSec(),
		SlotsPerEpoch:                        spectypes.BeaconTestNetwork.SlotsPerEpoch(),
		EpochsPerSyncCommitteePeriod:         256,
		SyncCommitteeSize:                    512,
		SyncCommitteeSubnetCount:             4,
		TargetAggregatorsPerSyncSubcommittee: 16,
		TargetAggregatorsPerCommittee:        16,
		IntervalsPerSlot:                     3,
		GenesisForkVersion:                   spectypes.BeaconTestNetwork.ForkVersion(),
		GenesisTime:                          time.Unix(int64(spectypes.BeaconTestNetwork.MinGenesisTime()), 0), // #nosec G115 -- time should not exceed int64
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
	SSV: &SSV{
		Name:                 "testnet",
		DomainType:           spectypes.DomainType{0x0, 0x0, spectypes.JatoNetworkID.Byte(), 0x2},
		RegistrySyncOffset:   new(big.Int).SetInt64(9015219),
		RegistryContractAddr: ethcommon.HexToAddress("0x4B133c68A084B8A88f72eDCd7944B69c8D545f03"),
		Bootnodes: []string{
			"enr:-Li4QFIQzamdvTxGJhvcXG_DFmCeyggSffDnllY5DiU47pd_K_1MRnSaJimWtfKJ-MD46jUX9TwgW5Jqe0t4pH41RYWGAYuFnlyth2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhCLdu_SJc2VjcDI1NmsxoQN4v-N9zFYwEqzGPBBX37q24QPFvAVUtokIo1fblIsmTIN0Y3CCE4uDdWRwgg-j",
		},
		TotalEthereumValidators: 1_000_000, // just some high enough value, so we never accidentally reach the message-limits derived from it while testing something with local testnet
		Forks: []SSVFork{
			{
				Name:  "alan",
				Epoch: 0, // Alan fork happened on another epoch, but we won't ever run pre-Alan fork again, so 0 should work fine
			},
		},
	},
}
