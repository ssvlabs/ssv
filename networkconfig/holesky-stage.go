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
		"enr:-Ja4QDWBZRHe_NL2iSUz4YrqWMjFdKezbK5t-Qcd3D0owyB2CL7sz1cU6-2N6ohhpYdqNFggGXTHIUDAO_HjA6_p2wqGAY_dYl_DgmlkgnY0gmlwhDQrLYqJc2VjcDI1NmsxoQKnAiuSlgSR8asjCH0aYoVKM8uPbi4noFuFHZHaAHqknYNzc3YBg3RjcIITiYN1ZHCCD6E",
	},
}
