package networkconfig

import (
	"math/big"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

var Mainnet = NetworkConfig{
	Name:                 "mainnet",
	Beacon:               beacon.NewNetwork(spectypes.MainNetwork),
	GenesisDomainType:    spectypes.GenesisMainnet,
	AlanDomainType:       spectypes.AlanMainnet,
	GenesisEpoch:         218450,
	AlanForkEpoch:        327168, // Nov-25-2024 12:00:23 PM UTC
	RegistrySyncOffset:   new(big.Int).SetInt64(17507487),
	RegistryContractAddr: "0xDD9BC35aE942eF0cFa76930954a156B3fF30a4E1",
	Bootnodes: []string{
		// Blox
		"enr:-Li4QHEPYASj5ZY3BXXKXAoWcoIw0ChgUlTtfOSxgNlYxlmpEWUR_K6Nr04VXsMpWSQxWWM4QHDyypnl92DQNpWkMS-GAYiWUvo8h2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhCzmKVSJc2VjcDI1NmsxoQOW29na1pUAQw4jF3g0zsPgJG89ViHJOOkHFFklnC2UyIN0Y3CCE4qDdWRwgg-i",

		// 0NEinfra bootnode
		"enr:-Li4QDwrOuhEq5gBJBzFUPkezoYiy56SXZUwkSD7bxYo8RAhPnHyS0de0nOQrzl-cL47RY9Jg8k6Y_MgaUd9a5baYXeGAYnfZE76h2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhDaTS0mJc2VjcDI1NmsxoQMZzUHaN3eClRgF9NAqRNc-ilGpJDDJxdenfo4j-zWKKYN0Y3CCE4iDdWRwgg-g",

		// Eridian (eridianalpha.com)
		"enr:-Li4QIzHQ2H82twhvsu8EePZ6CA1gl0_B0WWsKaT07245TkHUqXay-MXEgObJB7BxMFl8TylFxfnKNxQyGTXh-2nAlOGAYuraxUEh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhBKCzUSJc2VjcDI1NmsxoQNKskkQ6-mBdBWr_ORJfyHai5uD0vL6Fuw90X0sPwmRsoN0Y3CCE4iDdWRwgg-g",

		// CryptoManufaktur
		"enr:-Li4QH7FwJcL8gJj0zHAITXqghMkG-A5bfWh2-3Q7vosy9D1BS8HZk-1ITuhK_rfzG3v_UtBDI6uNJZWpdcWfrQFCxKGAYnQ1DRCh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhBLb3g2Jc2VjcDI1NmsxoQKeSDcZWSaY9FC723E9yYX1Li18bswhLNlxBZdLfgOKp4N0Y3CCE4mDdWRwgg-h",
	},
}
