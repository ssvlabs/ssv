package networkconfig

import (
	"math/big"
	"time"

	spectypes "github.com/bloxapp/ssv-spec/types"
)

var Mainnet = spectypes.BeaconNetwork{
	Name:                   "mainnet",
	DefaultSyncOffset:      new(big.Int).SetInt64(8661727),
	ForkVersion:            [4]byte{0, 0, 0, 0},
	MinGenesisTime:         1606824023,
	SlotDuration:           12 * time.Second,
	SlotsPerEpoch:          32,
	CapellaForkEpoch:       194048,
	Domain:                 spectypes.GenesisMainnet,
	DepositContractAddress: "0x00000000219ab540356cBB839Cbe05303d7705Fa",
	GenesisValidatorsRoot:  "4b363db94e286120d76eb905340fdd4e54bfe9f06bf33ff6cf5ad27f511bfe95",
	GenesisEpoch:           1,
}
