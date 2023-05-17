package networkconfig

import (
	"time"

	spectypes "github.com/bloxapp/ssv-spec/types"
)

var MainNetwork = spectypes.BeaconNetworkDescriptor{
	Name:              "mainnet",
	DefaultSyncOffset: "8661727",
	ForkVersion:       [4]byte{0, 0, 0, 0},
	MinGenesisTime:    1606824023,
	SlotDuration:      12 * time.Second,
}
