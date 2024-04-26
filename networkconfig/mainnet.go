package networkconfig

import (
	"math/big"

	spectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"

	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
)

var Mainnet = NetworkConfig{
	Name:                 "mainnet",
	Beacon:               beacon.NewNetwork(spectypes.MainNetwork),
	Domain:               spectypes.GenesisMainnet,
	GenesisEpoch:         218450,
	RegistrySyncOffset:   new(big.Int).SetInt64(17507487),
	RegistryContractAddr: "0xDD9BC35aE942eF0cFa76930954a156B3fF30a4E1",
	Bootnodes: []string{
		// Blox
		"enr:-Li4QHEPYASj5ZY3BXXKXAoWcoIw0ChgUlTtfOSxgNlYxlmpEWUR_K6Nr04VXsMpWSQxWWM4QHDyypnl92DQNpWkMS-GAYiWUvo8h2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhCzmKVSJc2VjcDI1NmsxoQOW29na1pUAQw4jF3g0zsPgJG89ViHJOOkHFFklnC2UyIN0Y3CCE4qDdWRwgg-i",

		// 0NEinfra bootnode
		"enr:-Li4QDwrOuhEq5gBJBzFUPkezoYiy56SXZUwkSD7bxYo8RAhPnHyS0de0nOQrzl-cL47RY9Jg8k6Y_MgaUd9a5baYXeGAYnfZE76h2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhDaTS0mJc2VjcDI1NmsxoQMZzUHaN3eClRgF9NAqRNc-ilGpJDDJxdenfo4j-zWKKYN0Y3CCE4iDdWRwgg-g",

		// Taiga
		"enr:-Li4QOg_lfX8uhSKGfm0RDbARe9j1ujim6JiQ-h8E1QB175DWIaGAvzXLxa-OsLjrX24zYstxMQkDHkQTdm-Qq406wuGAYj8K5H3h2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhJbmcQeJc2VjcDI1NmsxoQIYVg92mRyqn519Og6VA6fdgqeFxKgQO87IX64zJcmqhoN0Y3CCE4mDdWRwgg-h",

		// CryptoManufaktur
		"enr:-Li4QH7FwJcL8gJj0zHAITXqghMkG-A5bfWh2-3Q7vosy9D1BS8HZk-1ITuhK_rfzG3v_UtBDI6uNJZWpdcWfrQFCxKGAYnQ1DRCh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhBLb3g2Jc2VjcDI1NmsxoQKeSDcZWSaY9FC723E9yYX1Li18bswhLNlxBZdLfgOKp4N0Y3CCE4mDdWRwgg-h",
	},
}
