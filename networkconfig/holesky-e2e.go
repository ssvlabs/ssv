package networkconfig

import (
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
)

var HoleskyE2E = NetworkConfig{
	Name:                 "holesky-e2e",
	Beacon:               beacon.NewNetwork(spectypes.HoleskyNetwork),
	Domain:               spectypes.DomainType{0x0, 0x0, 0xee, 0x1},
	GenesisEpoch:         1,
	RegistryContractAddr: "0xC3CD9A0aE89Fff83b71b58b6512D43F8a41f363D",
	Bootnodes:            []string{},
}
