package networkconfig

import (
	"math/big"

	ethcommon "github.com/ethereum/go-ethereum/common"
)

var HoleskyStageSSV = SSV{
	Name:                 "holesky-stage",
	GenesisDomainType:    [4]byte{0x00, 0x00, 0x31, 0x12},
	AlanDomainType:       [4]byte{0x00, 0x00, 0x31, 0x13},
	GenesisEpoch:         1,
	RegistrySyncOffset:   new(big.Int).SetInt64(84599),
	RegistryContractAddr: ethcommon.HexToAddress("0x0d33801785340072C452b994496B19f196b7eE15"),
	AlanForkEpoch:        0,
	DiscoveryProtocolID:  [6]byte{'s', 's', 'v', 'd', 'v', '5'},
	Bootnodes: []string{
		// Public bootnode:
		// "enr:-Ja4QDYHVgUs9NvlMqq93ot6VNqbmrIlMrwKnq4X3DPRgyUNB4ospDp8ubMvsf-KsgqY8rzpZKy4GbE1DLphabpRBc-GAY_diLjngmlkgnY0gmlwhDQrLYqJc2VjcDI1NmsxoQKnAiuSlgSR8asjCH0aYoVKM8uPbi4noFuFHZHaAHqknYNzc3YBg3RjcIITiYN1ZHCCD6E",

		// Private bootnode:
		"enr:-Ja4QDRUBjWOvVfGxpxvv3FqaCy3psm7IsKu5ETb1GXiexGYDFppD33t7AHRfmQddoAkBiyb7pt4t7ZN0sNB9CsW4I-GAZGOmChMgmlkgnY0gmlwhAorXxuJc2VjcDI1NmsxoQP_bBE-ZYvaXKBR3dRYMN5K_lZP-q-YsBzDZEtxH_4T_YNzc3YBg3RjcIITioN1ZHCCD6I",
	},
}