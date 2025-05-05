package networkconfig

import (
	"fmt"
	"math"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

//go:generate go tool -modfile=../tool.mod mockgen -package=networkconfig -destination=./beacon_mock.go -source=./beacon.go

type Beacon interface {
	GetSlotStartTime(slot phase0.Slot) time.Time
	GetSlotEndTime(slot phase0.Slot) time.Time
	EstimatedCurrentSlot() phase0.Slot
	EstimatedSlotAtTime(time time.Time) phase0.Slot
	EstimatedCurrentEpoch() phase0.Epoch
	EstimatedEpochAtSlot(slot phase0.Slot) phase0.Epoch
	IsFirstSlotOfEpoch(slot phase0.Slot) bool
	GetEpochFirstSlot(epoch phase0.Epoch) phase0.Slot
	EpochsPerSyncCommitteePeriod() phase0.Epoch
	EstimatedSyncCommitteePeriodAtEpoch(epoch phase0.Epoch) uint64
	FirstEpochOfSyncPeriod(period uint64) phase0.Epoch
	LastSlotOfSyncPeriod(period uint64) phase0.Slot
	FirstSlotAtEpoch(epoch phase0.Epoch) phase0.Slot
	EpochStartTime(epoch phase0.Epoch) time.Time
	EstimatedTimeAtSlot(slot phase0.Slot) time.Time
	GetSlotDuration() time.Duration
	GetSlotsPerEpoch() phase0.Slot
	GetGenesisTime() time.Time
	GetBeaconName() string
}

type BeaconConfig struct {
	BeaconName    string
	SlotDuration  time.Duration
	SlotsPerEpoch phase0.Slot
	ForkVersion   phase0.Version
	GenesisTime   time.Time
}

// GetSlotStartTime returns the start time for the given slot
func (b BeaconConfig) GetSlotStartTime(slot phase0.Slot) time.Time {
	if slot > math.MaxInt64 {
		panic(fmt.Sprintf("slot %d out of range", slot))
	}
	durationSinceGenesisStart := time.Duration(slot) * b.SlotDuration // #nosec G115: slot cannot exceed math.MaxInt64
	start := b.GenesisTime.Add(durationSinceGenesisStart)
	return start
}

// GetSlotEndTime returns the end time for the given slot
func (b BeaconConfig) GetSlotEndTime(slot phase0.Slot) time.Time {
	return b.GetSlotStartTime(slot + 1)
}

// EstimatedCurrentSlot returns the estimation of the current slot
func (b BeaconConfig) EstimatedCurrentSlot() phase0.Slot {
	return b.EstimatedSlotAtTime(time.Now())
}

// EstimatedSlotAtTime estimates slot at the given time
func (b BeaconConfig) EstimatedSlotAtTime(time time.Time) phase0.Slot {
	if time.Before(b.GenesisTime) {
		panic(fmt.Sprintf("time %v is before genesis time %v", time, b.GenesisTime))
	}
	timeAfterGenesis := time.Sub(b.GenesisTime)
	return phase0.Slot(timeAfterGenesis / b.SlotDuration) // #nosec G115: genesis can't be negative
}

// EstimatedCurrentEpoch estimates the current epoch
// https://github.com/ethereum/eth2.0-specs/blob/dev/specs/phase0/beacon-chain.md#compute_start_slot_at_epoch
func (b BeaconConfig) EstimatedCurrentEpoch() phase0.Epoch {
	return b.EstimatedEpochAtSlot(b.EstimatedCurrentSlot())
}

// EstimatedEpochAtSlot estimates epoch at the given slot
func (b BeaconConfig) EstimatedEpochAtSlot(slot phase0.Slot) phase0.Epoch {
	return phase0.Epoch(slot / b.SlotsPerEpoch)
}

// IsFirstSlotOfEpoch estimates epoch at the given slot
func (b BeaconConfig) IsFirstSlotOfEpoch(slot phase0.Slot) bool {
	return slot%b.SlotsPerEpoch == 0
}

// GetEpochFirstSlot returns the beacon node first slot in epoch
func (b BeaconConfig) GetEpochFirstSlot(epoch phase0.Epoch) phase0.Slot {
	return phase0.Slot(epoch) * b.SlotsPerEpoch
}

// EpochsPerSyncCommitteePeriod returns the number of epochs per sync committee period.
func (b BeaconConfig) EpochsPerSyncCommitteePeriod() phase0.Epoch {
	return 256
}

// EstimatedSyncCommitteePeriodAtEpoch estimates the current sync committee period at the given Epoch
func (b BeaconConfig) EstimatedSyncCommitteePeriodAtEpoch(epoch phase0.Epoch) uint64 {
	return uint64(epoch / b.EpochsPerSyncCommitteePeriod())
}

// FirstEpochOfSyncPeriod calculates the first epoch of the given sync period.
func (b BeaconConfig) FirstEpochOfSyncPeriod(period uint64) phase0.Epoch {
	return phase0.Epoch(period) * b.EpochsPerSyncCommitteePeriod()
}

// LastSlotOfSyncPeriod calculates the first epoch of the given sync period.
func (b BeaconConfig) LastSlotOfSyncPeriod(period uint64) phase0.Slot {
	lastEpoch := b.FirstEpochOfSyncPeriod(period+1) - 1
	// If we are in the sync committee that ends at slot x we do not generate a message during slot x-1
	// as it will never be included, hence -1.
	return b.GetEpochFirstSlot(lastEpoch+1) - 2
}

func (b BeaconConfig) FirstSlotAtEpoch(epoch phase0.Epoch) phase0.Slot {
	return phase0.Slot(epoch) * b.SlotsPerEpoch
}

func (b BeaconConfig) EpochStartTime(epoch phase0.Epoch) time.Time {
	firstSlot := b.FirstSlotAtEpoch(epoch)
	t := b.EstimatedTimeAtSlot(firstSlot)
	return t
}

func (b BeaconConfig) EstimatedTimeAtSlot(slot phase0.Slot) time.Time {
	if slot > math.MaxInt64 {
		panic(fmt.Sprintf("slot %d out of range", slot))
	}
	d := time.Duration(slot) * b.SlotDuration // #nosec G115: slot cannot exceed math.MaxInt64
	return b.GenesisTime.Add(d)
}

func (b BeaconConfig) GetSlotDuration() time.Duration {
	return b.SlotDuration
}

func (b BeaconConfig) GetSlotsPerEpoch() phase0.Slot {
	return b.SlotsPerEpoch
}

func (b BeaconConfig) GetGenesisTime() time.Time {
	return b.GenesisTime
}

func (b BeaconConfig) GetBeaconName() string {
	return b.BeaconName
}
