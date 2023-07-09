package beacon

import (
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	spectypes "github.com/bloxapp/ssv-spec/types"
)

// Network is a beacon chain network.
type Network struct {
	spectypes.BeaconNetwork
	LocalTestNet bool
}

// NewNetwork creates a new beacon chain network.
func NewNetwork(network spectypes.BeaconNetwork) Network {
	return Network{
		BeaconNetwork: network,
		LocalTestNet:  false,
	}
}

// NewNetworkWithLocalTestNet creates a new beacon chain network.
func NewNetworkWithLocalTestNet(network spectypes.BeaconNetwork, localTestNet bool) Network {
	return Network{
		BeaconNetwork: network,
		LocalTestNet:  localTestNet,
	}
}

// CustomMinGenesisTime returns min genesis time value
func (n Network) CustomMinGenesisTime() uint64 {
	//TODO: get from config
	if n.LocalTestNet {
		return 1688914691
	}
	return n.BeaconNetwork.MinGenesisTime()
}

// GetSlotStartTime returns the start time for the given slot
func (n Network) GetSlotStartTime(slot phase0.Slot) time.Time {
	timeSinceGenesisStart := uint64(slot) * uint64(n.SlotDurationSec().Seconds())
	start := time.Unix(int64(n.CustomMinGenesisTime()+timeSinceGenesisStart), 0)
	return start
}

// EstimatedCurrentSlot returns the estimation of the current slot
func (n Network) EstimatedCurrentSlot() phase0.Slot {
	return n.EstimatedSlotAtTime(time.Now().Unix())
}

// EstimatedSlotAtTime estimates slot at the given time
func (n Network) EstimatedSlotAtTime(time int64) phase0.Slot {
	genesis := int64(n.CustomMinGenesisTime())
	if time < genesis {
		return 0
	}
	return phase0.Slot(uint64(time-genesis) / uint64(n.SlotDurationSec().Seconds()))
}

// EstimatedCurrentEpoch estimates the current epoch
// https://github.com/ethereum/eth2.0-specs/blob/dev/specs/phase0/beacon-chain.md#compute_start_slot_at_epoch
func (n Network) EstimatedCurrentEpoch() phase0.Epoch {
	return n.EstimatedEpochAtSlot(n.EstimatedCurrentSlot())
}

// EstimatedEpochAtSlot estimates epoch at the given slot
func (n Network) EstimatedEpochAtSlot(slot phase0.Slot) phase0.Epoch {
	return phase0.Epoch(slot / phase0.Slot(n.SlotsPerEpoch()))
}

// IsFirstSlotOfEpoch estimates epoch at the given slot
func (n Network) IsFirstSlotOfEpoch(slot phase0.Slot) bool {
	return uint64(slot)%n.SlotsPerEpoch() == 0
}

// GetEpochFirstSlot returns the beacon node first slot in epoch
func (n Network) GetEpochFirstSlot(epoch phase0.Epoch) phase0.Slot {
	return phase0.Slot(uint64(epoch) * n.SlotsPerEpoch())
}

// EstimatedSyncCommitteePeriodAtEpoch estimates the current sync committee period at the given Epoch
func (n Network) EstimatedSyncCommitteePeriodAtEpoch(epoch phase0.Epoch) uint64 {
	// TODO: consider extracting EpochsPerSyncCommitteePeriod to config
	return uint64(epoch) / 256 // EpochsPerSyncCommitteePeriod
}

// FirstEpochOfSyncPeriod calculates the first epoch of the given sync period.
func (n Network) FirstEpochOfSyncPeriod(period uint64) phase0.Epoch {
	return phase0.Epoch(period * 256) // EpochsPerSyncCommitteePeriod
}

// LastSlotOfSyncPeriod calculates the first epoch of the given sync period.
func (n Network) LastSlotOfSyncPeriod(period uint64) phase0.Slot {
	lastEpoch := n.FirstEpochOfSyncPeriod(period+1) - 1
	// If we are in the sync committee that ends at slot x we do not generate a message during slot x-1
	// as it will never be included, hence -1.
	return n.GetEpochFirstSlot(lastEpoch+1) - 2
}

func (n Network) String() string {
	return string(n.BeaconNetwork)
}

func (n Network) MarshalJSON() ([]byte, error) {
	return []byte(`"` + n.BeaconNetwork + `"`), nil
}

func (n *Network) UnmarshalJSON(b []byte) error {
	if len(b) < 2 {
		return nil
	}
	*n = NewNetwork(spectypes.BeaconNetwork(b[1 : len(b)-1]))
	return nil
}
