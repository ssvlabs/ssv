package networkconfig

import (
	"math/big"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

var HoleskyStage = NetworkConfig{
	Name:                 "holesky-stage",
	Beacon:               beacon.NewNetwork(spectypes.HoleskyNetwork),
	Domain:               [4]byte{0x00, 0x00, 0x31, 0x12},
	GenesisEpoch:         1,
	RegistrySyncOffset:   new(big.Int).SetInt64(84599),
	RegistryContractAddr: "0x0d33801785340072C452b994496B19f196b7eE15",
	Bootnodes: []string{
		"enr:-Li4QPnPGESWx2wnu3s2qeu6keFbkaV2M0ZiGHgxxGI9ThP4XSgSaFzl6zYsF1zAdni3Mh04iA6BEZqoC6LZ52UFnwKGAYxEgLqeh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhDQiKqmJc2VjcDI1NmsxoQP2e508AoA0B-KH-IaAd3nVCfI9q16lNztV-oTpcH72tIN0Y3CCE4mDdWRwgg-h",
	},
}
