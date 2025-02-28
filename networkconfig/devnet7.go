package networkconfig

import (
	"math/big"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

var Devnet7 = NetworkConfig{
	Name:                 "devnet7",
	Beacon:               beacon.NewNetwork(spectypes.Devnet7Network),
	DomainType:           spectypes.DomainType{0x77, 0x77, 0x77, 0x77},
	GenesisEpoch:         1,
	RegistrySyncOffset:   new(big.Int).SetInt64(10070),
	RegistryContractAddr: "0xad45A78180961079BFaeEe349704F411dfF947C6",
	DiscoveryProtocolID:  [6]byte{'s', 's', 'v', 'd', 'v', '5'},
	Bootnodes: []string{
		// SSV Labs
		"enr:-Ja4QJDLtOwNrAsTnHiaFp1DY0RdfQeGkTaUEe4LOkLfQEGWRxkQv6iafatb8JJaYmE0D-Lh1TrDKFGJW6fGrmgyk_SGAZVMniVzgmlkgnY0gmlwhAorlcuJc2VjcDI1NmsxoQP_bBE-ZYvaXKBR3dRYMN5K_lZP-q-YsBzDZEtxH_4T_YNzc3YBg3RjcIITioN1ZHCCD6I",
	},
}
