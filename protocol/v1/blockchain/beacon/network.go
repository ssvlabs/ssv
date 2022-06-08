package beacon

import (
	"github.com/bloxapp/eth2-key-manager/core"
	"time"
)

// Network is a beacon chain network.
type Network struct {
	core.Network
}

// NewNetwork creates a new beacon chain network.
func NewNetwork(net core.Network) Network {
	return Network{net}
}

// GetSlotStartTime returns the start time for the given slot
func (n *Network) GetSlotStartTime(slot uint64) time.Time {
	timeSinceGenesisStart := slot * uint64(n.SlotDurationSec().Seconds())
	start := time.Unix(int64(n.MinGenesisTime()+timeSinceGenesisStart), 0)
	return start
}
