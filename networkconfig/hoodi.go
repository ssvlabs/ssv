package networkconfig

import (
	"math/big"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

var Hoodi = NetworkConfig{
	Name: "hoodi",
	BeaconConfig: BeaconConfig{
		Beacon: beacon.NewNetwork(spectypes.HoodiNetwork),
	},
	SSVConfig: SSVConfig{
		DomainType:           spectypes.DomainType{0x0, 0x0, 0x5, 0x3},
		RegistrySyncOffset:   new(big.Int).SetInt64(1065),
		RegistryContractAddr: "0x58410Bef803ECd7E63B23664C586A6DB72DAf59c",
		DiscoveryProtocolID:  [6]byte{'s', 's', 'v', 'd', 'v', '5'},
		Bootnodes: []string{
			// SSV Labs
			"enr:-Ja4QIKlyNFuFtTOnVoavqwmpgSJXfhSmhpdSDOUhf5-FBr7bBxQRvG6VrpUvlkr8MtpNNuMAkM33AseduSaOhd9IeWGAZWjRbnvgmlkgnY0gmlwhCNVVTCJc2VjcDI1NmsxoQNTTyiJPoZh502xOZpHSHAfR-94NaXLvi5J4CNHMh2tjoNzc3YBg3RjcIITioN1ZHCCD6I",
		},
	},
}
