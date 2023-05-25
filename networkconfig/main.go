package networkconfig

import (
	"math/big"
	"time"

	"github.com/bloxapp/eth2-key-manager/core"
	spectypes "github.com/bloxapp/ssv-spec/types"
)

var Mainnet = spectypes.BeaconNetwork{
	Name: "mainnet",
	SSV: spectypes.SSVParams{
		Domain:               spectypes.GenesisMainnet,
		ForkVersion:          [4]byte{0, 0, 0, 0},
		GenesisEpoch:         1,
		ETH1SyncOffset:       new(big.Int).SetInt64(8661727),
		RegistryContractAddr: "", // TODO: set up
		Bootnodes:            []string{
			// TODO: fill
		},
	},
	ETH: spectypes.ETHParams{
		NetworkName:      string(core.MainNetwork),
		MinGenesisTime:   1606824023,
		SlotDuration:     12 * time.Second,
		SlotsPerEpoch:    32,
		CapellaForkEpoch: 194048,
	},
}
