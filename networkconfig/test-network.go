package networkconfig

import (
	"math/big"

	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
)

var TestNetwork = NetworkConfig{
	Name:                 "testnet",
	Beacon:               beacon.NewNetwork(spectypes.PraterNetwork),
	Domain:               spectypes.JatoTestnet,
	GenesisEpoch:         152834,
	RegistrySyncOffset:   new(big.Int).SetInt64(9015219),
	RegistryContractAddr: "0x4B133c68A084B8A88f72eDCd7944B69c8D545f03",
	Bootnodes: []string{
		"enr:-Li4QPnPGESWx2wnu3s2qeu6keFbkaV2M0ZiGHgxxGI9ThP4XSgSaFzl6zYsF1zAdni3Mh04iA6BEZqoC6LZ52UFnwKGAYxEgLqeh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhDQiKqmJc2VjcDI1NmsxoQP2e508AoA0B-KH-IaAd3nVCfI9q16lNztV-oTpcH72tIN0Y3CCE4mDdWRwgg-h",
	},
	PermissionlessActivationEpoch: 123456789,
}
