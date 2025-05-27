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

var Mainnet = NetworkConfig{
	Name: "mainnet",
	BeaconConfig: BeaconConfig{
		BeaconName:                           string(spectypes.MainNetwork),
		SlotDuration:                         spectypes.MainNetwork.SlotDurationSec(),
		SlotsPerEpoch:                        spectypes.MainNetwork.SlotsPerEpoch(),
		EpochsPerSyncCommitteePeriod:         256,
		SyncCommitteeSize:                    512,
		SyncCommitteeSubnetCount:             4,
		TargetAggregatorsPerSyncSubcommittee: 16,
		TargetAggregatorsPerCommittee:        16,
		IntervalsPerSlot:                     3,
		GenesisForkVersion:                   spectypes.MainNetwork.ForkVersion(),
		GenesisTime:                          time.Unix(int64(spectypes.MainNetwork.MinGenesisTime()), 0), // #nosec G115 -- time should not exceed int64
		GenesisValidatorsRoot:                phase0.Root(hexutil.MustDecode("0x4b363db94e286120d76eb905340fdd4e54bfe9f06bf33ff6cf5ad27f511bfe95")),
		Forks: map[spec.DataVersion]phase0.Fork{
			// Phase0 (genesis)
			spec.DataVersionPhase0: {
				Epoch:           phase0.Epoch(0),
				PreviousVersion: phase0.Version{0x00, 0x00, 0x00, 0x00}, // GENESIS_FORK_VERSION
				CurrentVersion:  phase0.Version{0x00, 0x00, 0x00, 0x00},
			},
			// Altair @ epoch 74240
			spec.DataVersionAltair: {
				Epoch:           phase0.Epoch(74240),
				PreviousVersion: phase0.Version{0x00, 0x00, 0x00, 0x00},
				CurrentVersion:  phase0.Version{0x01, 0x00, 0x00, 0x00}, // ALTAIR_FORK_VERSION
			},
			// Bellatrix (Merge) @ epoch 144896
			spec.DataVersionBellatrix: {
				Epoch:           phase0.Epoch(144896),
				PreviousVersion: phase0.Version{0x01, 0x00, 0x00, 0x00},
				CurrentVersion:  phase0.Version{0x02, 0x00, 0x00, 0x00}, // BELLATRIX_FORK_VERSION
			},
			// Capella @ epoch 194048
			spec.DataVersionCapella: {
				Epoch:           phase0.Epoch(194048),
				PreviousVersion: phase0.Version{0x02, 0x00, 0x00, 0x00},
				CurrentVersion:  phase0.Version{0x03, 0x00, 0x00, 0x00}, // CAPELLA_FORK_VERSION
			},
			// Deneb @ epoch 269568
			spec.DataVersionDeneb: {
				Epoch:           phase0.Epoch(269568),
				PreviousVersion: phase0.Version{0x03, 0x00, 0x00, 0x00},
				CurrentVersion:  phase0.Version{0x04, 0x00, 0x00, 0x00}, // DENEB_FORK_VERSION
			},
			// Electra @ epoch 364032
			spec.DataVersionElectra: {
				Epoch:           phase0.Epoch(364032),
				PreviousVersion: phase0.Version{0x04, 0x00, 0x00, 0x00},
				CurrentVersion:  phase0.Version{0x05, 0x00, 0x00, 0x00}, // ELECTRA_FORK_VERSION
			},
		},
	},
	SSVConfig: SSVConfig{
		DomainType:           spectypes.AlanMainnet,
		RegistrySyncOffset:   new(big.Int).SetInt64(17507487),
		RegistryContractAddr: ethcommon.HexToAddress("0xDD9BC35aE942eF0cFa76930954a156B3fF30a4E1"),
		DiscoveryProtocolID:  [6]byte{'s', 's', 'v', 'd', 'v', '5'},
		Bootnodes: []string{
			// SSV Labs
			"enr:-Ja4QAbDe5XANqJUDyJU1GmtS01qqMwDYx9JNZgymjBb55fMaha80E2HznRYoUGy6NFVSvs1u1cFqSM0MgJI-h1QKLeGAZKaTo7LgmlkgnY0gmlwhDQrfraJc2VjcDI1NmsxoQNEj0Pgq9-VxfeX83LPDOUPyWiTVzdI-DnfMdO1n468u4Nzc3YBg3RjcIITioN1ZHCCD6I",

			// 0NEinfra bootnode
			"enr:-Li4QDwrOuhEq5gBJBzFUPkezoYiy56SXZUwkSD7bxYo8RAhPnHyS0de0nOQrzl-cL47RY9Jg8k6Y_MgaUd9a5baYXeGAYnfZE76h2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhDaTS0mJc2VjcDI1NmsxoQMZzUHaN3eClRgF9NAqRNc-ilGpJDDJxdenfo4j-zWKKYN0Y3CCE4iDdWRwgg-g",

			// Eridian (eridianalpha.com)
			"enr:-Li4QIzHQ2H82twhvsu8EePZ6CA1gl0_B0WWsKaT07245TkHUqXay-MXEgObJB7BxMFl8TylFxfnKNxQyGTXh-2nAlOGAYuraxUEh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhBKCzUSJc2VjcDI1NmsxoQNKskkQ6-mBdBWr_ORJfyHai5uD0vL6Fuw90X0sPwmRsoN0Y3CCE4iDdWRwgg-g",

			// CryptoManufaktur
			"enr:-Li4QH7FwJcL8gJj0zHAITXqghMkG-A5bfWh2-3Q7vosy9D1BS8HZk-1ITuhK_rfzG3v_UtBDI6uNJZWpdcWfrQFCxKGAYnQ1DRCh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhBLb3g2Jc2VjcDI1NmsxoQKeSDcZWSaY9FC723E9yYX1Li18bswhLNlxBZdLfgOKp4N0Y3CCE4mDdWRwgg-h",
		},
		TotalEthereumValidators: 1064860, // active_validators from https://mainnet.beaconcha.in/index/data on Apr 18, 2025
	},
}
