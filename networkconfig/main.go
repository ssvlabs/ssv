package networkconfig

import (
	"math/big"
	"time"

	spectypes "github.com/bloxapp/ssv-spec/types"
)

var MainNetwork = spectypes.BeaconNetwork{
	Name:              "mainnet",
	DefaultSyncOffset: new(big.Int).SetInt64(8661727),
	ForkVersion:       [4]byte{0, 0, 0, 0},
	MinGenesisTime:    1606824023,
	SlotDuration:      12 * time.Second,
	CapellaForkEpoch:  194048,
}
