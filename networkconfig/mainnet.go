package networkconfig

import (
	"math/big"
	"time"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

var Mainnet = NetworkConfig{
	Name: "mainnet",
	BeaconConfig: BeaconConfig{
		BeaconName:    string(spectypes.MainNetwork),
		SlotDuration:  spectypes.MainNetwork.SlotDurationSec(),
		SlotsPerEpoch: spectypes.MainNetwork.SlotsPerEpoch(),
		ForkVersion:   spectypes.MainNetwork.ForkVersion(),
		GenesisTime:   time.Unix(int64(spectypes.MainNetwork.MinGenesisTime()), 0), // #nosec G115 -- time should not exceed int64
	},
	SSVConfig: SSVConfig{
		DomainType:           spectypes.AlanMainnet,
		RegistrySyncOffset:   new(big.Int).SetInt64(17507487),
		RegistryContractAddr: "0xDD9BC35aE942eF0cFa76930954a156B3fF30a4E1",
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
	},
}
