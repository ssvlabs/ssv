package networkconfig

import (
	"math/big"

	ethcommon "github.com/ethereum/go-ethereum/common"
)

const HoodiStageName = "hoodi-stage"

var HoodiStageSSV = &SSVConfig{
	DomainType:           [4]byte{0x00, 0x00, 0x31, 0x14},
	RegistrySyncOffset:   new(big.Int).SetInt64(1004),
	RegistryContractAddr: ethcommon.HexToAddress("0x0aaace4e8affc47c6834171c88d342a4abd8f105"),
	DiscoveryProtocolID:  [6]byte{'s', 's', 'v', 'd', 'v', '5'},
	Bootnodes: []string{
		// SSV Labs
		"enr:-Ja4QJZcaYfS0GpX-5xREVBa26a-E-QHMFek-EndsJdgM6loIM7pfbJwPDCNK1VzPkUhMjwcTTuNASiHU6X-sjsrxFmGAZWjNu06gmlkgnY0gmlwhErcGnyJc2VjcDI1NmsxoQP_bBE-ZYvaXKBR3dRYMN5K_lZP-q-YsBzDZEtxH_4T_YNzc3YBg3RjcIITioN1ZHCCD6I",
	},
	TotalEthereumValidators: HoodiSSV.TotalEthereumValidators,
	Forks: SSVForks{
		Alan:            0,
		GasLimit36:      0,
		NetworkTopology: 29293,
	},
}
