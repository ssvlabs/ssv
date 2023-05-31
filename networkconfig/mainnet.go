package networkconfig

import (
	"math/big"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
)

var Mainnet = NetworkConfig{
	Name:                 "mainnet",
	Beacon:               beacon.NewNetwork(spectypes.MainNetwork),
	Domain:               spectypes.GenesisMainnet,
	GenesisEpoch:         1,
	ETH1SyncOffset:       new(big.Int).SetInt64(8661727),
	RegistryContractAddr: "", // TODO: set up
	Bootnodes:            []string{
		// TODO: fill
	},
}
