package networkconfig

import (
	"math/big"
	"time"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ethcommon "github.com/ethereum/go-ethereum/common"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

var Sepolia = NetworkConfig{
	Name: "sepolia",
	BeaconConfig: BeaconConfig{
		BeaconName:                           string(spectypes.SepoliaNetwork),
		SlotDuration:                         spectypes.SepoliaNetwork.SlotDurationSec(),
		SlotsPerEpoch:                        spectypes.SepoliaNetwork.SlotsPerEpoch(),
		EpochsPerSyncCommitteePeriod:         256,
		SyncCommitteeSize:                    512,
		SyncCommitteeSubnetCount:             4,
		TargetAggregatorsPerSyncSubcommittee: 16,
		TargetAggregatorsPerCommittee:        16,
		IntervalsPerSlot:                     3,
		GenesisForkVersion:                   spectypes.SepoliaNetwork.ForkVersion(),
		GenesisTime:                          time.Unix(int64(spectypes.SepoliaNetwork.MinGenesisTime()), 0), // #nosec G115 -- time should not exceed int64
		Forks: map[spec.DataVersion]phase0.Fork{
			// Phase0 (genesis)
			spec.DataVersionPhase0: {
				Epoch:           phase0.Epoch(0),
				PreviousVersion: phase0.Version{0x90, 0x00, 0x00, 0x69}, // GENESIS_FORK_VERSION
				CurrentVersion:  phase0.Version{0x90, 0x00, 0x00, 0x69},
			},
			// Altair @ epoch 50
			spec.DataVersionAltair: {
				Epoch:           phase0.Epoch(50),
				PreviousVersion: phase0.Version{0x90, 0x00, 0x00, 0x69},
				CurrentVersion:  phase0.Version{0x90, 0x00, 0x00, 0x70}, // ALTAIR_FORK_VERSION
			},
			// Bellatrix (Merge) @ epoch 100
			spec.DataVersionBellatrix: {
				Epoch:           phase0.Epoch(100),
				PreviousVersion: phase0.Version{0x90, 0x00, 0x00, 0x70},
				CurrentVersion:  phase0.Version{0x90, 0x00, 0x00, 0x71}, // BELLATRIX_FORK_VERSION
			},
			// Capella @ epoch 56832
			spec.DataVersionCapella: {
				Epoch:           phase0.Epoch(56832),
				PreviousVersion: phase0.Version{0x90, 0x00, 0x00, 0x71},
				CurrentVersion:  phase0.Version{0x90, 0x00, 0x00, 0x72}, // CAPELLA_FORK_VERSION
			},
			// Deneb @ epoch 132608
			spec.DataVersionDeneb: {
				Epoch:           phase0.Epoch(132608),
				PreviousVersion: phase0.Version{0x90, 0x00, 0x00, 0x72},
				CurrentVersion:  phase0.Version{0x90, 0x00, 0x00, 0x73}, // DENEB_FORK_VERSION
			},
			// Electra @ epoch 222464
			spec.DataVersionElectra: {
				Epoch:           phase0.Epoch(222464),
				PreviousVersion: phase0.Version{0x90, 0x00, 0x00, 0x73},
				CurrentVersion:  phase0.Version{0x90, 0x00, 0x00, 0x74}, // ELECTRA_FORK_VERSION
			},
		},
	},
	SSVConfig: SSVConfig{
		DomainType:           spectypes.DomainType{0x0, 0x0, 0x5, 0x69},
		RegistrySyncOffset:   new(big.Int).SetInt64(7795814),
		RegistryContractAddr: ethcommon.HexToAddress("0x261419B48F36EdF420743E9f91bABF4856e76f99"),
		DiscoveryProtocolID:  [6]byte{'s', 's', 'v', 'd', 'v', '5'},
		Bootnodes: []string{
			// SSV Labs
			"enr:-Ja4QIE0Ml0a8Pq9zD-0g9KYGN3jAMPJ0CAP0i16fK-PSHfLeORl-Z5p8odoP1oS5S2E8IsF5jNG7gqTKhjVsHR-Z_CGAZXrnTJrgmlkgnY0gmlwhCOjXGWJc2VjcDI1NmsxoQKCRDQsIdFsJDmu_ZU2H6b2_HRJbuUneDXHLfFkSQH9O4Nzc3YBg3RjcIITioN1ZHCCD6I",
		},
		TotalEthereumValidators: 1781, // active_validators from https://sepolia.beaconcha.in/index/data on Mar 20, 2025
	},
}
