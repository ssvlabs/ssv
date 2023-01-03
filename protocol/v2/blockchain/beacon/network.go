package beacon

import (
	"github.com/bloxapp/eth2-key-manager/core"
	types "github.com/prysmaticlabs/eth2-types"
	prysmTime "github.com/prysmaticlabs/prysm/time"
	"time"
)

// Network is a beacon chain network.
type Network struct {
	core.Network
	minGenesisTime uint64
}

// NewNetwork creates a new beacon chain network.
func NewNetwork(network core.Network, minGenesisTime uint64) Network {
	return Network{network, minGenesisTime}
}

// GetSlotStartTime returns the start time for the given slot
func (n *Network) GetSlotStartTime(slot uint64) time.Time {
	timeSinceGenesisStart := slot * uint64(n.SlotDurationSec().Seconds())
	start := time.Unix(int64(n.MinGenesisTime()+timeSinceGenesisStart), 0)
	return start
}

func (n Network) MinGenesisTime() uint64 {
	if n.minGenesisTime > 0 {
		return n.minGenesisTime
	} else {
		return n.Network.MinGenesisTime()
	}
}

// EstimatedCurrentSlot returns the estimation of the current slot
func (n Network) EstimatedCurrentSlot() types.Slot {
	return n.EstimatedSlotAtTime(prysmTime.Now().Unix())
}

// EstimatedSlotAtTime estimates slot at the given time
func (n Network) EstimatedSlotAtTime(time int64) types.Slot {
	genesis := int64(n.MinGenesisTime())
	if time < genesis {
		return 0
	}
	return types.Slot(uint64(time-genesis) / uint64(n.SlotDurationSec().Seconds()))
}

// EstimatedCurrentEpoch estimates the current epoch
// https://github.com/ethereum/eth2.0-specs/blob/dev/specs/phase0/beacon-chain.md#compute_start_slot_at_epoch
func (n Network) EstimatedCurrentEpoch() types.Epoch {
	return n.EstimatedEpochAtSlot(n.EstimatedCurrentSlot())
}

// EstimatedEpochAtSlot estimates epoch at the given slot
func (n Network) EstimatedEpochAtSlot(slot types.Slot) types.Epoch {
	return types.Epoch(slot / types.Slot(n.SlotsPerEpoch()))
}
