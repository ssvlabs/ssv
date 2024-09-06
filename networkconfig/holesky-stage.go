package networkconfig

import (
	"math/big"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

var HoleskyStage = NetworkConfig{
	Name:                 "holesky-stage",
	Beacon:               beacon.NewNetwork(spectypes.HoleskyNetwork),
	GenesisDomainType:    [4]byte{0x00, 0x00, 0x31, 0x12},
	AlanDomainType:       [4]byte{0x00, 0x00, 0x31, 0x13},
	GenesisEpoch:         1,
	RegistrySyncOffset:   new(big.Int).SetInt64(84599),
	RegistryContractAddr: "0x0d33801785340072C452b994496B19f196b7eE15",
	AlanForkEpoch:        99999999,
	Bootnodes: []string{
		// Public bootnode:
		// "enr:-Ja4QDYHVgUs9NvlMqq93ot6VNqbmrIlMrwKnq4X3DPRgyUNB4ospDp8ubMvsf-KsgqY8rzpZKy4GbE1DLphabpRBc-GAY_diLjngmlkgnY0gmlwhDQrLYqJc2VjcDI1NmsxoQKnAiuSlgSR8asjCH0aYoVKM8uPbi4noFuFHZHaAHqknYNzc3YBg3RjcIITiYN1ZHCCD6E",

		// Private bootnode:
		"enr:-Ja4QEF4O52Pl9pxfF1y_gBzmtp9s2-ncWoN2-VPebEvjibaGJH7VpYGZKdCVws_gIAkjjatn67rXhGuItCfcp2kv4SGAZGOmChHgmlkgnY0gmlwhAoqckWJc2VjcDI1NmsxoQP_bBE-ZYvaXKBR3dRYMN5K_lZP-q-YsBzDZEtxH_4T_YNzc3YBg3RjcIITioN1ZHCCD6I",
	},
}
