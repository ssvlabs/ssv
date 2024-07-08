package networkconfig

import (
	"math/big"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

var HoleskyE2E = NetworkConfig{
	Name:                 "holesky-e2e",
	Beacon:               beacon.NewNetwork(spectypes.HoleskyNetwork),
	GenesisEpoch:         1,
	RegistryContractAddr: "0x58410bef803ecd7e63b23664c586a6db72daf59c",
	RegistrySyncOffset:   big.NewInt(405579),
	Bootnodes:            []string{},
}
