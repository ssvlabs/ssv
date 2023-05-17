package networkconfig

import (
	"time"

	spectypes "github.com/bloxapp/ssv-spec/types"
)

var PraterV3TestnetNetwork = spectypes.BeaconNetwork{
	Name:              "prater",
	DefaultSyncOffset: "8661727",
	ForkVersion:       [4]byte{0x00, 0x00, 0x10, 0x20},
	MinGenesisTime:    1616508000,
	SlotDuration:      12 * time.Second,
	CapellaForkEpoch:  162304,
}
