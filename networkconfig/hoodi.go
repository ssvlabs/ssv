package networkconfig

import (
	"math/big"

	ethcommon "github.com/ethereum/go-ethereum/common"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

const HoodiName = "hoodi"

var HoodiSSV = SSVConfig{
	DomainType:           spectypes.DomainType{0x0, 0x0, 0x5, 0x3},
	RegistrySyncOffset:   new(big.Int).SetInt64(1065),
	RegistryContractAddr: ethcommon.HexToAddress("0x58410Bef803ECd7E63B23664C586A6DB72DAf59c"),
	DiscoveryProtocolID:  [6]byte{'s', 's', 'v', 'd', 'v', '5'},
	Bootnodes: []string{
		// SSV Labs
		"enr:-Ja4QIKlyNFuFtTOnVoavqwmpgSJXfhSmhpdSDOUhf5-FBr7bBxQRvG6VrpUvlkr8MtpNNuMAkM33AseduSaOhd9IeWGAZWjRbnvgmlkgnY0gmlwhCNVVTCJc2VjcDI1NmsxoQNTTyiJPoZh502xOZpHSHAfR-94NaXLvi5J4CNHMh2tjoNzc3YBg3RjcIITioN1ZHCCD6I",
	},
	TotalEthereumValidators: 1107955, // active_validators from https://hoodi.beaconcha.in/index/data on Apr 18, 2025
}
