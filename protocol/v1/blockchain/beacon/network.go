package beacon

import (
	"github.com/bloxapp/eth2-key-manager/core"
	"time"
)

type Network struct {
	core.Network
}

func NewNetwork(net core.Network) Network {
	return Network{net}
}

// GetSlotStartTime returns the start time for the given slot  TODO: redundant func (in ssvNode) need to fix
func (n *Network) GetSlotStartTime(slot uint64) time.Time {
	timeSinceGenesisStart := slot * uint64(n.SlotDurationSec().Seconds())
	start := time.Unix(int64(n.MinGenesisTime()+timeSinceGenesisStart), 0)
	return start
}
