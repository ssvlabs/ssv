package networkconfig

import (
	"math/big"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

var Mekong = NetworkConfig{
	Name:                 "mekong",
	Beacon:               beacon.NewNetwork(spectypes.MekongNetwork),
	DomainType:           spectypes.DomainType{0x0, 0x0, 0x5, 0x1},
	GenesisEpoch:         1,
	RegistrySyncOffset:   new(big.Int).SetInt64(12609),
	RegistryContractAddr: "0xa2A44482e752D3D956AAFaA94CFDD21Aacfc2959",
	DiscoveryProtocolID:  [6]byte{'s', 's', 'v', 'd', 'v', '5'},
	Bootnodes: []string{
		// SSV Labs
		"enr:-Ja4QH_GPnnueKug32sk3KyhyiCDrkGsrW11TXoLlCiAraT-Ky0C-SvCpqixJUalNjVF8P7PTeORGQuH-z2jRLIITx-GAZTQMa9fgmlkgnY0gmlwhAor0qKJc2VjcDI1NmsxoQP_bBE-ZYvaXKBR3dRYMN5K_lZP-q-YsBzDZEtxH_4T_YNzc3YBg3RjcIITioN1ZHCCD6I"},
}