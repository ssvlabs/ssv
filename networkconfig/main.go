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
		Domain:                 spectypes.GenesisMainnet,
		ForkVersion:            [4]byte{0, 0, 0, 0},
		GenesisEpoch:           1,
		DefaultSyncOffset:      new(big.Int).SetInt64(8661727),
		DepositContractAddress: "0x00000000219ab540356cBB839Cbe05303d7705Fa",
		GenesisValidatorsRoot:  "4b363db94e286120d76eb905340fdd4e54bfe9f06bf33ff6cf5ad27f511bfe95",
		Bootnodes:              []string{
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
