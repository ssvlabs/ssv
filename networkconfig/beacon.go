package networkconfig

import (
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

type Beacon struct {
	// TODO: try to get rid of methods and use the values directly
	ConfigNameVal                   string         `json:"config_name" yaml:"ConfigName"`
	GenesisForkVersionVal           phase0.Version `json:"genesis_fork_version" yaml:"GenesisForkVersion"`
	CapellaForkVersionVal           phase0.Version `json:"capella_fork_version" yaml:"CapellaForkVersion"`
	MinGenesisTimeVal               time.Time      `json:"min_genesis_time" yaml:"MinGenesisTime"`
	SlotDurationVal                 time.Duration  `json:"slot_duration" yaml:"SlotDuration"`
	SlotsPerEpochVal                phase0.Slot    `json:"slots_per_epoch" yaml:"SlotsPerEpoch"`
	EpochsPerSyncCommitteePeriodVal phase0.Epoch   `json:"epochs_per_sync_committee_period" yaml:"EpochsPerSyncCommitteePeriod"`
}

func (b Beacon) String() string {
	encoded, err := json.Marshal(b)
	if err != nil {
		return fmt.Sprintf("<malformed: %v>", err)
	}

	return string(encoded)
}

func (b Beacon) ConfigName() string {
	return b.ConfigNameVal
}

func (b Beacon) GenesisForkVersion() phase0.Version {
	return b.GenesisForkVersionVal
}

func (b Beacon) CapellaForkVersion() phase0.Version {
	return b.CapellaForkVersionVal
}

func (b Beacon) MinGenesisTime() time.Time {
	return b.MinGenesisTimeVal
}

func (b Beacon) SlotDuration() time.Duration {
	return b.SlotDurationVal
}

func (b Beacon) SlotsPerEpoch() phase0.Slot {
	return b.SlotsPerEpochVal
}

// EpochsPerSyncCommitteePeriod returns the number of epochs per sync committee period.
func (b Beacon) EpochsPerSyncCommitteePeriod() phase0.Epoch {
	return b.EpochsPerSyncCommitteePeriodVal
}

// GetSlotStartTime returns the start time for the given slot
func (b Beacon) GetSlotStartTime(slot phase0.Slot) time.Time {
	if slot > math.MaxInt64 {
		panic("slot out of range")
	}
	durationSinceGenesisStart := time.Duration(slot) * b.SlotDuration() // #nosec G115: slot cannot exceed math.MaxInt64
	return b.MinGenesisTime().Add(durationSinceGenesisStart)
}

func (b Beacon) EstimatedTimeAtSlot(slot phase0.Slot) time.Time {
	if slot > math.MaxInt64 {
		panic("slot out of range")
	}
	d := time.Duration(slot) * b.SlotDuration() // #nosec G115: slot cannot exceed math.MaxInt64
	return b.MinGenesisTime().Add(d)
}

func (b Beacon) FirstSlotAtEpoch(epoch phase0.Epoch) phase0.Slot {
	return phase0.Slot(epoch) * b.SlotsPerEpoch()
}

func (b Beacon) EpochStartTime(epoch phase0.Epoch) time.Time {
	firstSlot := b.FirstSlotAtEpoch(epoch)
	return b.EstimatedTimeAtSlot(firstSlot)
}

// GetSlotEndTime returns the end time for the given slot
func (b Beacon) GetSlotEndTime(slot phase0.Slot) time.Time {
	return b.GetSlotStartTime(slot + 1)
}

// EstimatedCurrentSlot returns the estimation of the current slot
func (b Beacon) EstimatedCurrentSlot() phase0.Slot {
	return b.EstimatedSlotAtTime(time.Now())
}

// EstimatedSlotAtTime estimates slot at the given time
func (b Beacon) EstimatedSlotAtTime(time time.Time) phase0.Slot {
	genesis := b.MinGenesisTime()
	if time.Before(genesis) {
		return 0
	}
	return phase0.Slot(time.Sub(genesis) / b.SlotDuration()) // #nosec G115: genesis can't be negative
}

// EstimatedCurrentEpoch estimates the current epoch
// https://github.com/ethereum/eth2.0-specs/blob/dev/specs/phase0/beacon-chain.md#compute_start_slot_at_epoch
func (b Beacon) EstimatedCurrentEpoch() phase0.Epoch {
	return b.EstimatedEpochAtSlot(b.EstimatedCurrentSlot())
}

// EstimatedEpochAtSlot estimates epoch at the given slot
func (b Beacon) EstimatedEpochAtSlot(slot phase0.Slot) phase0.Epoch {
	return phase0.Epoch(slot / b.SlotsPerEpoch())
}

// IsFirstSlotOfEpoch estimates epoch at the given slot
func (b Beacon) IsFirstSlotOfEpoch(slot phase0.Slot) bool {
	return slot%b.SlotsPerEpoch() == 0
}

// GetEpochFirstSlot returns the beacon node first slot in epoch
func (b Beacon) GetEpochFirstSlot(epoch phase0.Epoch) phase0.Slot {
	return phase0.Slot(epoch) * b.SlotsPerEpoch()
}

// EstimatedSyncCommitteePeriodAtEpoch estimates the current sync committee period at the given Epoch
func (b Beacon) EstimatedSyncCommitteePeriodAtEpoch(epoch phase0.Epoch) uint64 {
	return uint64(epoch / b.EpochsPerSyncCommitteePeriod())
}

// FirstEpochOfSyncPeriod calculates the first epoch of the given sync period.
func (b Beacon) FirstEpochOfSyncPeriod(period uint64) phase0.Epoch {
	return phase0.Epoch(period) * b.EpochsPerSyncCommitteePeriod()
}

// LastSlotOfSyncPeriod calculates the first epoch of the given sync period.
func (b Beacon) LastSlotOfSyncPeriod(period uint64) phase0.Slot {
	lastEpoch := b.FirstEpochOfSyncPeriod(period+1) - 1
	// If we are in the sync committee that ends at slot x we do not generate a message during slot x-1
	// as it will never be included, hence -1.
	return b.GetEpochFirstSlot(lastEpoch+1) - 2
}

func (b Beacon) EstimatedCurrentEpochStartTime() time.Time {
	return b.GetSlotStartTime(b.GetEpochFirstSlot(b.EstimatedCurrentEpoch()))
}
