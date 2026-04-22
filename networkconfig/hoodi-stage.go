package networkconfig

import (
	"math/big"

	ethcommon "github.com/ethereum/go-ethereum/common"
)

var HoodiStageSSV = &SSV{
	Name:                 "hoodi-stage",
	DomainType:           [4]byte{0x00, 0x00, 0x31, 0x14},
	RegistrySyncOffset:   new(big.Int).SetInt64(1004),
	RegistryContractAddr: ethcommon.HexToAddress("0xc07B3E9671f884FDa67E1e7D43d952E0e1369fd8"),
	DiscoveryProtocolID:  [6]byte{'s', 's', 'v', 'd', 'v', '5'},
	Bootnodes: []string{
		// SSV Labs
		"enr:-Ja4QJZcaYfS0GpX-5xREVBa26a-E-QHMFek-EndsJdgM6loIM7pfbJwPDCNK1VzPkUhMjwcTTuNASiHU6X-sjsrxFmGAZWjNu06gmlkgnY0gmlwhErcGnyJc2VjcDI1NmsxoQP_bBE-ZYvaXKBR3dRYMN5K_lZP-q-YsBzDZEtxH_4T_YNzc3YBg3RjcIITioN1ZHCCD6I",
	},
	TotalEthereumValidators: HoodiSSV.TotalEthereumValidators,
	Forks:                   SSVForks{},
}
