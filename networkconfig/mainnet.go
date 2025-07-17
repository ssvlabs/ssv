package networkconfig

import (
	"math/big"

	ethcommon "github.com/ethereum/go-ethereum/common"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

var MainnetSSV = &SSV{
	Name:                 "mainnet",
	DomainType:           spectypes.AlanMainnet,
	RegistrySyncOffset:   new(big.Int).SetInt64(17507487),
	RegistryContractAddr: ethcommon.HexToAddress("0xDD9BC35aE942eF0cFa76930954a156B3fF30a4E1"),
	DiscoveryProtocolID:  [6]byte{'s', 's', 'v', 'd', 'v', '5'},
	Bootnodes: []string{
		// SSV Labs
		"enr:-Ja4QAbDe5XANqJUDyJU1GmtS01qqMwDYx9JNZgymjBb55fMaha80E2HznRYoUGy6NFVSvs1u1cFqSM0MgJI-h1QKLeGAZKaTo7LgmlkgnY0gmlwhDQrfraJc2VjcDI1NmsxoQNEj0Pgq9-VxfeX83LPDOUPyWiTVzdI-DnfMdO1n468u4Nzc3YBg3RjcIITioN1ZHCCD6I",

		// 0NEinfra bootnode
		"enr:-Ja4QFpDBTMnLOykFvZsV8LnBibOQnhAgsN2SkOXApEcRbxyJNkHO9go3gonVUIREbUa0gHvzYdgzZ3U4ezmCyJrkI6GAZfeUw2QgmlkgnY0gmlwhId9yoSJc2VjcDI1NmsxoQMZzUHaN3eClRgF9NAqRNc-ilGpJDDJxdenfo4j-zWKKYNzc3YBg3RjcIITiIN1ZHCCD6A",

		// Eridian (eridianalpha.com)
		"enr:-Li4QIzHQ2H82twhvsu8EePZ6CA1gl0_B0WWsKaT07245TkHUqXay-MXEgObJB7BxMFl8TylFxfnKNxQyGTXh-2nAlOGAYuraxUEh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhBKCzUSJc2VjcDI1NmsxoQNKskkQ6-mBdBWr_ORJfyHai5uD0vL6Fuw90X0sPwmRsoN0Y3CCE4iDdWRwgg-g",

		// CryptoManufaktur
		"enr:-Li4QH7FwJcL8gJj0zHAITXqghMkG-A5bfWh2-3Q7vosy9D1BS8HZk-1ITuhK_rfzG3v_UtBDI6uNJZWpdcWfrQFCxKGAYnQ1DRCh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhBLb3g2Jc2VjcDI1NmsxoQKeSDcZWSaY9FC723E9yYX1Li18bswhLNlxBZdLfgOKp4N0Y3CCE4mDdWRwgg-h",
	},
	TotalEthereumValidators: 1064860, // active_validators from https://mainnet.beaconcha.in/index/data on Apr 18, 2025
	GasLimit36Epoch:         385150,  // Aug-09-2025 06:40:23 AM UTC
	Forks: []SSVFork{
		{
			Name:  "alan",
			Epoch: 0, // Alan fork happened on another epoch, but we won't ever run pre-Alan fork again, so 0 should work fine
		},
	},
}
