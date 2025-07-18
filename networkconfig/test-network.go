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

var TestNetwork = &NetworkConfig{
	Name: "testnet",
	BeaconConfig: &BeaconConfig{
		NetworkName:                          string(spectypes.BeaconTestNetwork),
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
	SSVConfig: &SSVConfig{
		DomainType:           spectypes.DomainType{0x0, 0x0, spectypes.JatoNetworkID.Byte(), 0x2},
		RegistrySyncOffset:   new(big.Int).SetInt64(9015219),
		RegistryContractAddr: ethcommon.HexToAddress("0x4B133c68A084B8A88f72eDCd7944B69c8D545f03"),
		Bootnodes: []string{
			"enr:-Li4QFIQzamdvTxGJhvcXG_DFmCeyggSffDnllY5DiU47pd_K_1MRnSaJimWtfKJ-MD46jUX9TwgW5Jqe0t4pH41RYWGAYuFnlyth2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhCLdu_SJc2VjcDI1NmsxoQN4v-N9zFYwEqzGPBBX37q24QPFvAVUtokIo1fblIsmTIN0Y3CCE4uDdWRwgg-j",
		},
		TotalEthereumValidators: 1_000_000, // just some high enough value, so we never accidentally reach the message-limits derived from it while testing something with local testnet
		Forks: []SSVFork{
			{
				Name:  AlanFork,
				Epoch: 0,
			},
		},
	},
}

// TestRealNetwork is used in tests that require real network parameters.
var TestRealNetwork = &NetworkConfig{
	Name: "holesky",
	BeaconConfig: &BeaconConfig{
		NetworkName:                          string(spectypes.HoleskyNetwork),
		SlotDuration:                         spectypes.HoleskyNetwork.SlotDurationSec(),
		SlotsPerEpoch:                        spectypes.HoleskyNetwork.SlotsPerEpoch(),
		EpochsPerSyncCommitteePeriod:         256,
		SyncCommitteeSize:                    512,
		SyncCommitteeSubnetCount:             4,
		TargetAggregatorsPerSyncSubcommittee: 16,
		TargetAggregatorsPerCommittee:        16,
		IntervalsPerSlot:                     3,
		GenesisForkVersion:                   spectypes.HoleskyNetwork.ForkVersion(),
		GenesisTime:                          time.Unix(int64(spectypes.HoleskyNetwork.MinGenesisTime()), 0), // #nosec G115 -- time should not exceed int64
		GenesisValidatorsRoot:                phase0.Root(hexutil.MustDecode("0x9143aa7c615a7f7115e2b6aac319c03529df8242ae705fba9df39b79c59fa8b1")),
		Forks: map[spec.DataVersion]phase0.Fork{
			// Phase0
			spec.DataVersionPhase0: {
				Epoch:           phase0.Epoch(0),
				PreviousVersion: phase0.Version{0x00, 0x01, 0x70, 0x00}, // GENESIS_FORK_VERSION: 0x01017000
				CurrentVersion:  phase0.Version{0x00, 0x01, 0x70, 0x00},
			},
			// Altair @ epoch 0
			spec.DataVersionAltair: {
				Epoch:           phase0.Epoch(0),
				PreviousVersion: phase0.Version{0x00, 0x01, 0x70, 0x00},
				CurrentVersion:  phase0.Version{0x02, 0x01, 0x70, 0x00}, // ALTAIR_FORK_VERSION: 0x02017000
			},
			// Bellatrix @ epoch 0
			spec.DataVersionBellatrix: {
				Epoch:           phase0.Epoch(0),
				PreviousVersion: phase0.Version{0x02, 0x01, 0x70, 0x00},
				CurrentVersion:  phase0.Version{0x03, 0x01, 0x70, 0x00}, // BELLATRIX_FORK_VERSION: 0x03017000
			},
			// Capella @ epoch 256
			spec.DataVersionCapella: {
				Epoch:           phase0.Epoch(256),
				PreviousVersion: phase0.Version{0x03, 0x01, 0x70, 0x00},
				CurrentVersion:  phase0.Version{0x04, 0x01, 0x70, 0x00}, // CAPELLA_FORK_VERSION: 0x04017000
			},
			// Deneb @ epoch 29 696
			spec.DataVersionDeneb: {
				Epoch:           phase0.Epoch(29696),
				PreviousVersion: phase0.Version{0x04, 0x01, 0x70, 0x00},
				CurrentVersion:  phase0.Version{0x05, 0x01, 0x70, 0x00}, // DENEB_FORK_VERSION: 0x05017000
			},
			// Electra @ epoch 115 968
			spec.DataVersionElectra: {
				Epoch:           phase0.Epoch(115968),
				PreviousVersion: phase0.Version{0x05, 0x01, 0x70, 0x00},
				CurrentVersion:  phase0.Version{0x06, 0x01, 0x70, 0x00}, // ELECTRA_FORK_VERSION: 0x06017000
			},
		},
	},
	SSVConfig: &SSVConfig{
		DomainType:           spectypes.DomainType{0x0, 0x0, 0x5, 0x2},
		RegistrySyncOffset:   new(big.Int).SetInt64(181612),
		RegistryContractAddr: ethcommon.HexToAddress("0x38A4794cCEd47d3baf7370CcC43B560D3a1beEFA"),
		DiscoveryProtocolID:  [6]byte{'s', 's', 'v', 'd', 'v', '5'},
		Bootnodes: []string{
			// SSV Labs
			"enr:-Ja4QKFD3u5tZob7xukp-JKX9QJMFqqI68cItsE4tBbhsOyDR0M_1UUjb35hbrqvTP3bnXO_LnKh-jNLTeaUqN4xiduGAZKaP_sagmlkgnY0gmlwhDb0fh6Jc2VjcDI1NmsxoQMw_H2anuiqP9NmEaZwbUfdvPFog7PvcKmoVByDa576SINzc3YBg3RjcIITioN1ZHCCD6I",
		},
		TotalEthereumValidators: 1757795, // active_validators from https://holesky.beaconcha.in/index/data on Nov 20, 2024
		Forks: []SSVFork{
			{
				Name:  AlanFork,
				Epoch: 0,
			},
		},
	},
}
