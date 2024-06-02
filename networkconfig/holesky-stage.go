package networkconfig

import (
	"math/big"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

var HoleskyStage = NetworkConfig{
	Name:                 "holesky-stage",
	Beacon:               beacon.NewNetwork(spectypes.HoleskyNetwork),
	Domain:               [4]byte{0x00, 0x00, 0x31, 0x99}, // TODO: (Alan) revert
	GenesisEpoch:         1,
	RegistrySyncOffset:   new(big.Int).SetInt64(84599),
	RegistryContractAddr: "0x0d33801785340072C452b994496B19f196b7eE15",
	Bootnodes: []string{
		"enr:-Ja4QMFMjrYCXeunSw96P_hxKOxGddsTzFwxJj8hjzPQC73BN9ng_IE5DQvbUYitXSU_6m8mYsaM15gKhtcm5chUFWCGAY_ZreHKgmlkgnY0gmlwhDQiKqmJc2VjcDI1NmsxoQJ2M-diOqYDd5-7jdnmAz9LP0MLsH6g_r4PCTMPfCuXMINzc3YBg3RjcIITiYN1ZHCCD6E",
	},
}
