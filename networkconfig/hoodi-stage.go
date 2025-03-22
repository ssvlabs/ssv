package networkconfig

import (
	"math/big"

	ethcommon "github.com/ethereum/go-ethereum/common"
)

var HoodiStageSSV = SSV{
	Name:                    "hoodi-stage",
	DomainType:              [4]byte{0x00, 0x00, 0x31, 0x14},
	RegistrySyncOffset:      new(big.Int).SetInt64(1004),
	RegistryContractAddr:    ethcommon.HexToAddress("0x868C2789045d7ffC144635Da7D8cC0a974C58f89"),
	DiscoveryProtocolID:     [6]byte{'s', 's', 'v', 'd', 'v', '5'},
	TotalEthereumValidators: 2_000_000, // TODO: find the value and define
	Bootnodes: []string{
		// SSV Labs
		"enr:-Ja4QIKlyNFuFtTOnVoavqwmpgSJXfhSmhpdSDOUhf5-FBr7bBxQRvG6VrpUvlkr8MtpNNuMAkM33AseduSaOhd9IeWGAZWjRbnvgmlkgnY0gmlwhCNVVTCJc2VjcDI1NmsxoQNTTyiJPoZh502xOZpHSHAfR-94NaXLvi5J4CNHMh2tjoNzc3YBg3RjcIITioN1ZHCCD6I",
	},
}
