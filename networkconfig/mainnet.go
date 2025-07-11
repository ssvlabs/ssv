package networkconfig

import (
	"math"
	"math/big"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ethcommon "github.com/ethereum/go-ethereum/common"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

const MainnetName = "mainnet"

var MainnetSSV = &SSV{
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
	// TODO - set proper value for mainnet
	GasLimit36Epoch: phase0.Epoch(math.MaxUint64),
}
