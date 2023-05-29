package networkconfig

import (
	"math/big"

	spectypes "github.com/bloxapp/ssv-spec/types"
)

var Mainnet = NetworkConfig{
	Name:                 "mainnet",
	BeaconNetwork:        spectypes.MainNetwork,
	Domain:               spectypes.GenesisMainnet,
	GenesisEpoch:         1,
	ETH1SyncOffset:       new(big.Int).SetInt64(8661727),
	RegistryContractAddr: "", // TODO: set up
	Bootnodes:            []string{
		// TODO: fill
	},
}
