package networkconfig

import (
	"math/big"
	"time"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ethcommon "github.com/ethereum/go-ethereum/common"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

var MainnetSSV = SSV{
	Name:                      "mainnet",
	DomainType:                spectypes.AlanMainnet,
	RegistrySyncOffset:        new(big.Int).SetInt64(17507487),
	RegistryContractAddr:      ethcommon.HexToAddress("0xDD9BC35aE942eF0cFa76930954a156B3fF30a4E1"),
	MaxValidatorsPerCommittee: 560,
	TotalEthereumValidators:   1072679, // active_validators from https://mainnet.beaconcha.in/index/data on Nov 20, 2024
	Bootnodes: []string{
		// SSV Labs
		"enr:-Ja4QAbDe5XANqJUDyJU1GmtS01qqMwDYx9JNZgymjBb55fMaha80E2HznRYoUGy6NFVSvs1u1cFqSM0MgJI-h1QKLeGAZKaTo7LgmlkgnY0gmlwhDQrfraJc2VjcDI1NmsxoQNEj0Pgq9-VxfeX83LPDOUPyWiTVzdI-DnfMdO1n468u4Nzc3YBg3RjcIITioN1ZHCCD6I",
		"enr:-Li4QHEPYASj5ZY3BXXKXAoWcoIw0ChgUlTtfOSxgNlYxlmpEWUR_K6Nr04VXsMpWSQxWWM4QHDyypnl92DQNpWkMS-GAYiWUvo8h2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhCzmKVSJc2VjcDI1NmsxoQOW29na1pUAQw4jF3g0zsPgJG89ViHJOOkHFFklnC2UyIN0Y3CCE4qDdWRwgg-i",

		// 0NEinfra bootnode
		"enr:-Li4QDwrOuhEq5gBJBzFUPkezoYiy56SXZUwkSD7bxYo8RAhPnHyS0de0nOQrzl-cL47RY9Jg8k6Y_MgaUd9a5baYXeGAYnfZE76h2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhDaTS0mJc2VjcDI1NmsxoQMZzUHaN3eClRgF9NAqRNc-ilGpJDDJxdenfo4j-zWKKYN0Y3CCE4iDdWRwgg-g",

		// Eridian (eridianalpha.com)
		"enr:-Li4QIzHQ2H82twhvsu8EePZ6CA1gl0_B0WWsKaT07245TkHUqXay-MXEgObJB7BxMFl8TylFxfnKNxQyGTXh-2nAlOGAYuraxUEh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhBKCzUSJc2VjcDI1NmsxoQNKskkQ6-mBdBWr_ORJfyHai5uD0vL6Fuw90X0sPwmRsoN0Y3CCE4iDdWRwgg-g",

		// CryptoManufaktur
		"enr:-Li4QH7FwJcL8gJj0zHAITXqghMkG-A5bfWh2-3Q7vosy9D1BS8HZk-1ITuhK_rfzG3v_UtBDI6uNJZWpdcWfrQFCxKGAYnQ1DRCh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhBLb3g2Jc2VjcDI1NmsxoQKeSDcZWSaY9FC723E9yYX1Li18bswhLNlxBZdLfgOKp4N0Y3CCE4mDdWRwgg-h",
	},
}

var MainnetBeaconConfig = Beacon{
	ConfigName:                           string(spectypes.MainNetwork),
	CapellaForkVersion:                   phase0.Version{0x03, 0x00, 0x00, 0x00},
	SlotDuration:                         12 * time.Second,
	SlotsPerEpoch:                        32,
	EpochsPerSyncCommitteePeriod:         256,
	SyncCommitteeSize:                    512,
	SyncCommitteeSubnetCount:             4,
	TargetAggregatorsPerSyncSubcommittee: 16,
	TargetAggregatorsPerCommittee:        16,
	IntervalsPerSlot:                     3,
	Genesis: v1.Genesis{
		GenesisTime:           time.Unix(1606824023, 0),
		GenesisValidatorsRoot: phase0.Root{0x4B, 0x36, 0x3D, 0xB9, 0x4E, 0x28, 0x61, 0x20, 0xD7, 0x6E, 0xB9, 0x05, 0x34, 0x0F, 0xDD, 0x4E, 0x54, 0xBF, 0xE9, 0xF0, 0x6B, 0xF3, 0x3F, 0xF6, 0xCF, 0x5A, 0xD2, 0x7F, 0x51, 0x1B, 0xFE, 0x95},
		GenesisForkVersion:    phase0.Version{0, 0, 0, 0},
	},
}

var MainnetConfig = NetworkConfig{
	SSV:    MainnetSSV,
	Beacon: MainnetBeaconConfig,
}
