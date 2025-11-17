package common

import (
	"time"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common/hexutil"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
)

// MainnetBeaconConfig is the beacon configuration for the mainnet
var MainnetBeaconConfig = &networkconfig.Beacon{
	Name:                                 string(spectypes.MainNetwork),
	SlotDuration:                         spectypes.MainNetwork.SlotDurationSec(),
	SlotsPerEpoch:                        spectypes.MainNetwork.SlotsPerEpoch(),
	EpochsPerSyncCommitteePeriod:         256,
	SyncCommitteeSize:                    512,
	SyncCommitteeSubnetCount:             4,
	TargetAggregatorsPerSyncSubcommittee: 16,
	TargetAggregatorsPerCommittee:        16,
	GenesisForkVersion:                   spectypes.MainNetwork.ForkVersion(),
	GenesisTime:                          time.Unix(int64(spectypes.MainNetwork.MinGenesisTime()), 0), // #nosec G115 -- time should not exceed int64
	GenesisValidatorsRoot:                phase0.Root(hexutil.MustDecode("0x4b363db94e286120d76eb905340fdd4e54bfe9f06bf33ff6cf5ad27f511bfe95")),
	Forks: map[spec.DataVersion]phase0.Fork{
		// Phase0
		spec.DataVersionPhase0: {
			Epoch:           phase0.Epoch(0),
			PreviousVersion: phase0.Version{0x00, 0x00, 0x00, 0x00}, // GENESIS_FORK_VERSION: 0x00000000
			CurrentVersion:  phase0.Version{0x00, 0x00, 0x00, 0x00},
		},
		// Altair @ epoch 74240
		spec.DataVersionAltair: {
			Epoch:           phase0.Epoch(74240),
			PreviousVersion: phase0.Version{0x00, 0x00, 0x00, 0x00},
			CurrentVersion:  phase0.Version{0x01, 0x00, 0x00, 0x00}, // ALTAIR_FORK_VERSION: 0x01000000
		},
		// Bellatrix @ epoch 144896
		spec.DataVersionBellatrix: {
			Epoch:           phase0.Epoch(144896),
			PreviousVersion: phase0.Version{0x01, 0x00, 0x00, 0x00},
			CurrentVersion:  phase0.Version{0x02, 0x00, 0x00, 0x00}, // BELLATRIX_FORK_VERSION: 0x02000000
		},
		// Capella @ epoch 194048
		spec.DataVersionCapella: {
			Epoch:           phase0.Epoch(194048),
			PreviousVersion: phase0.Version{0x02, 0x00, 0x00, 0x00},
			CurrentVersion:  phase0.Version{0x03, 0x00, 0x00, 0x00}, // CAPELLA_FORK_VERSION: 0x03000000
		},
		// Deneb @ epoch 269568
		spec.DataVersionDeneb: {
			Epoch:           phase0.Epoch(269568),
			PreviousVersion: phase0.Version{0x03, 0x00, 0x00, 0x00},
			CurrentVersion:  phase0.Version{0x04, 0x00, 0x00, 0x00}, // DENEB_FORK_VERSION: 0x04000000
		},
		// Electra @ epoch 364032
		spec.DataVersionElectra: {
			Epoch:           phase0.Epoch(364032),
			PreviousVersion: phase0.Version{0x04, 0x00, 0x00, 0x00},
			CurrentVersion:  phase0.Version{0x05, 0x00, 0x00, 0x00}, // ELECTRA_FORK_VERSION: 0x05000000
		},
		// Fulu @ epoch 411392
		spec.DataVersionFulu: {
			Epoch:           phase0.Epoch(411392),
			PreviousVersion: phase0.Version{0x05, 0x00, 0x00, 0x00},
			CurrentVersion:  phase0.Version{0x06, 0x00, 0x00, 0x00}, // FULU_FORK_VERSION: 0x06000000
		},
	},
}
