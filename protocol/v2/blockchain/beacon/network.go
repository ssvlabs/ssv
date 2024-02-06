package beacon

import (
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
)

//go:generate mockgen -package=mocks -destination=./mocks/network.go -source=./network.go

// TODO: refactor type names to be more descriptive

// Network is a beacon chain network.
type Network struct {
	SpecNetwork
}

type BeaconNetwork interface {
	SpecNetwork

	GetSlotStartTime(slot phase0.Slot) time.Time
	GetSlotEndTime(slot phase0.Slot) time.Time
	IsFirstSlotOfEpoch(slot phase0.Slot) bool
	GetEpochFirstSlot(epoch phase0.Epoch) phase0.Slot

	EpochsPerSyncCommitteePeriod() uint64
	EstimatedSyncCommitteePeriodAtEpoch(epoch phase0.Epoch) uint64
	FirstEpochOfSyncPeriod(period uint64) phase0.Epoch
	LastSlotOfSyncPeriod(period uint64) phase0.Slot

	GetNetwork() Network
	GetBeaconNetwork() spectypes.BeaconNetwork

	String() string
}

type SpecNetwork interface {
	ForkVersion() [4]byte
	MinGenesisTime() uint64
	SlotDurationSec() time.Duration
	SlotsPerEpoch() uint64
	EstimatedCurrentSlot() phase0.Slot
	EstimatedSlotAtTime(time int64) phase0.Slot
	EstimatedTimeAtSlot(slot phase0.Slot) int64
	EstimatedCurrentEpoch() phase0.Epoch
	EstimatedEpochAtSlot(slot phase0.Slot) phase0.Epoch
	FirstSlotAtEpoch(epoch phase0.Epoch) phase0.Slot
	EpochStartTime(epoch phase0.Epoch) time.Time
}

// SpecNetworkWrapper wraps spectypes.BeaconNetwork and implements json.Marshaler and json.Unmarshaler.
// It is used for JSON encoding and decoding of spectypes.BeaconNetwork.
type SpecNetworkWrapper struct {
	spectypes.BeaconNetwork
}

func (n *SpecNetworkWrapper) String() string {
	return string(n.BeaconNetwork)
}

func (n *SpecNetworkWrapper) MarshalJSON() ([]byte, error) {
	return []byte(`"` + n.String() + `"`), nil
}

func (n *SpecNetworkWrapper) UnmarshalJSON(b []byte) error {
	if len(b) < 2 {
		return nil
	}

	*n = SpecNetworkWrapper{spectypes.BeaconNetwork(b[1 : len(b)-1])}
	return nil
}

// NewNetwork creates a new beacon chain network.
func NewNetwork(network SpecNetwork) Network {
	if _, ok := network.(spectypes.BeaconNetwork); ok {
		network = SpecNetworkWrapper{network.(spectypes.BeaconNetwork)}
	}

	return Network{
		SpecNetwork: network,
	}
}

// GetNetwork returns the network
func (n Network) GetNetwork() Network {
	return n
}

// GetBeaconNetwork returns the beacon network the node is on
func (n Network) GetBeaconNetwork() spectypes.BeaconNetwork {
	switch v := n.SpecNetwork.(type) {
	case SpecNetworkWrapper:
		return v.BeaconNetwork
	default:
		panic("TODO: implement")
	}
}

// GetSlotStartTime returns the start time for the given slot
func (n Network) GetSlotStartTime(slot phase0.Slot) time.Time {
	timeSinceGenesisStart := uint64(slot) * uint64(n.SlotDurationSec().Seconds())
	start := time.Unix(int64(n.MinGenesisTime()+timeSinceGenesisStart), 0)
	return start
}

// GetSlotEndTime returns the end time for the given slot
func (n Network) GetSlotEndTime(slot phase0.Slot) time.Time {
	return n.GetSlotStartTime(slot + 1)
}

// EstimatedCurrentSlot returns the estimation of the current slot
func (n Network) EstimatedCurrentSlot() phase0.Slot {
	return n.EstimatedSlotAtTime(time.Now().Unix())
}

// EstimatedSlotAtTime estimates slot at the given time
func (n Network) EstimatedSlotAtTime(time int64) phase0.Slot {
	genesis := int64(n.MinGenesisTime())
	if time < genesis {
		return 0
	}
	return phase0.Slot(uint64(time-genesis) / uint64(n.SpecNetwork.SlotDurationSec().Seconds()))
}

// EstimatedCurrentEpoch estimates the current epoch
// https://github.com/ethereum/eth2.0-specs/blob/dev/specs/phase0/beacon-chain.md#compute_start_slot_at_epoch
func (n Network) EstimatedCurrentEpoch() phase0.Epoch {
	return n.EstimatedEpochAtSlot(n.EstimatedCurrentSlot())
}

// EstimatedEpochAtSlot estimates epoch at the given slot
func (n Network) EstimatedEpochAtSlot(slot phase0.Slot) phase0.Epoch {
	return phase0.Epoch(slot / phase0.Slot(n.SpecNetwork.SlotsPerEpoch()))
}

// IsFirstSlotOfEpoch estimates epoch at the given slot
func (n Network) IsFirstSlotOfEpoch(slot phase0.Slot) bool {
	return uint64(slot)%n.SpecNetwork.SlotsPerEpoch() == 0
}

// GetEpochFirstSlot returns the beacon node first slot in epoch
func (n Network) GetEpochFirstSlot(epoch phase0.Epoch) phase0.Slot {
	return phase0.Slot(uint64(epoch) * n.SpecNetwork.SlotsPerEpoch())
}

// EpochsPerSyncCommitteePeriod returns the number of epochs per sync committee period.
func (n Network) EpochsPerSyncCommitteePeriod() uint64 {
	return 256
}

// EstimatedSyncCommitteePeriodAtEpoch estimates the current sync committee period at the given Epoch
func (n Network) EstimatedSyncCommitteePeriodAtEpoch(epoch phase0.Epoch) uint64 {
	return uint64(epoch) / n.EpochsPerSyncCommitteePeriod()
}

// FirstEpochOfSyncPeriod calculates the first epoch of the given sync period.
func (n Network) FirstEpochOfSyncPeriod(period uint64) phase0.Epoch {
	return phase0.Epoch(period * n.EpochsPerSyncCommitteePeriod())
}

// LastSlotOfSyncPeriod calculates the first epoch of the given sync period.
func (n Network) LastSlotOfSyncPeriod(period uint64) phase0.Slot {
	lastEpoch := n.FirstEpochOfSyncPeriod(period+1) - 1
	// If we are in the sync committee that ends at slot x we do not generate a message during slot x-1
	// as it will never be included, hence -1.
	return n.GetEpochFirstSlot(lastEpoch+1) - 2
}

func (n Network) String() string {
	switch v := n.SpecNetwork.(type) {
	case spectypes.BeaconNetwork:
		return string(v)
	case SpecNetworkWrapper:
		return v.String()
	default:
		panic("unknown network type")
	}
}
