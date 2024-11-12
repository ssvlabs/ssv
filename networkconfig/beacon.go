package networkconfig

import (
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

type BeaconConfig struct {
	GenesisForkVersionVal           phase0.Version `json:"genesis_fork_version" yaml:"GenesisForkVersion,omitempty"`
	CapellaForkVersionVal           phase0.Version `json:"capella_fork_version" yaml:"CapellaForkVersion"`
	MinGenesisTimeVal               time.Time      `json:"min_genesis_time" yaml:"MinGenesisTime"`
	SlotDurationVal                 time.Duration  `json:"slot_duration" yaml:"SlotDuration"`
	SlotsPerEpochVal                phase0.Slot    `json:"slots_per_epoch" yaml:"SlotsPerEpoch"`
	EpochsPerSyncCommitteePeriodVal phase0.Epoch   `json:"epochs_per_sync_committee_period" yaml:"EpochsPerSyncCommitteePeriod"`
}

func (bc BeaconConfig) String() string {
	b, err := json.Marshal(bc)
	if err != nil {
		return fmt.Sprintf("ERR: %v", err)
	}

	return string(b)
}

func (bc BeaconConfig) GenesisForkVersion() phase0.Version {
	return bc.GenesisForkVersionVal
}

func (bc BeaconConfig) MinGenesisTime() time.Time {
	return bc.MinGenesisTimeVal
}

func (bc BeaconConfig) SlotDuration() time.Duration {
	return bc.SlotDurationVal
}

func (bc BeaconConfig) SlotsPerEpoch() phase0.Slot {
	return bc.SlotsPerEpochVal
}

// EpochsPerSyncCommitteePeriod returns the number of epochs per sync committee period.
func (bc BeaconConfig) EpochsPerSyncCommitteePeriod() phase0.Epoch {
	return bc.EpochsPerSyncCommitteePeriodVal
}

// GetSlotStartTime returns the start time for the given slot
func (bc BeaconConfig) GetSlotStartTime(slot phase0.Slot) time.Time {
	if slot > math.MaxInt64 {
		panic("slot out of range")
	}
	durationSinceGenesisStart := time.Duration(slot) * bc.SlotDuration() // #nosec G115: slot cannot exceed math.MaxInt64
	return bc.MinGenesisTime().Add(durationSinceGenesisStart)
}

func (bc BeaconConfig) EstimatedTimeAtSlot(slot phase0.Slot) time.Time {
	if slot > math.MaxInt64 {
		panic("slot out of range")
	}
	d := time.Duration(slot) * bc.SlotDuration() // #nosec G115: slot cannot exceed math.MaxInt64
	return bc.MinGenesisTime().Add(d)
}

func (bc BeaconConfig) FirstSlotAtEpoch(epoch phase0.Epoch) phase0.Slot {
	return phase0.Slot(epoch) * bc.SlotsPerEpoch()
}

func (bc BeaconConfig) EpochStartTime(epoch phase0.Epoch) time.Time {
	firstSlot := bc.FirstSlotAtEpoch(epoch)
	return bc.EstimatedTimeAtSlot(firstSlot)
}

// GetSlotEndTime returns the end time for the given slot
func (bc BeaconConfig) GetSlotEndTime(slot phase0.Slot) time.Time {
	return bc.GetSlotStartTime(slot + 1)
}

// EstimatedCurrentSlot returns the estimation of the current slot
func (bc BeaconConfig) EstimatedCurrentSlot() phase0.Slot {
	return bc.EstimatedSlotAtTime(time.Now())
}

// EstimatedSlotAtTime estimates slot at the given time
func (bc BeaconConfig) EstimatedSlotAtTime(time time.Time) phase0.Slot {
	genesis := bc.MinGenesisTime()
	if time.Before(genesis) {
		return 0
	}
	return phase0.Slot(time.Sub(genesis) / bc.SlotDuration()) // #nosec G115: genesis can't be negative
}

// EstimatedCurrentEpoch estimates the current epoch
// https://github.com/ethereum/eth2.0-specs/blob/dev/specs/phase0/beacon-chain.md#compute_start_slot_at_epoch
func (bc BeaconConfig) EstimatedCurrentEpoch() phase0.Epoch {
	return bc.EstimatedEpochAtSlot(bc.EstimatedCurrentSlot())
}

// EstimatedEpochAtSlot estimates epoch at the given slot
func (bc BeaconConfig) EstimatedEpochAtSlot(slot phase0.Slot) phase0.Epoch {
	return phase0.Epoch(slot / bc.SlotsPerEpoch())
}

// IsFirstSlotOfEpoch estimates epoch at the given slot
func (bc BeaconConfig) IsFirstSlotOfEpoch(slot phase0.Slot) bool {
	return slot%bc.SlotsPerEpoch() == 0
}

// GetEpochFirstSlot returns the beacon node first slot in epoch
func (bc BeaconConfig) GetEpochFirstSlot(epoch phase0.Epoch) phase0.Slot {
	return phase0.Slot(epoch) * bc.SlotsPerEpoch()
}

// EstimatedSyncCommitteePeriodAtEpoch estimates the current sync committee period at the given Epoch
func (bc BeaconConfig) EstimatedSyncCommitteePeriodAtEpoch(epoch phase0.Epoch) uint64 {
	return uint64(epoch / bc.EpochsPerSyncCommitteePeriod())
}

// FirstEpochOfSyncPeriod calculates the first epoch of the given sync period.
func (bc BeaconConfig) FirstEpochOfSyncPeriod(period uint64) phase0.Epoch {
	return phase0.Epoch(period) * bc.EpochsPerSyncCommitteePeriod()
}

// LastSlotOfSyncPeriod calculates the first epoch of the given sync period.
func (bc BeaconConfig) LastSlotOfSyncPeriod(period uint64) phase0.Slot {
	lastEpoch := bc.FirstEpochOfSyncPeriod(period+1) - 1
	// If we are in the sync committee that ends at slot x we do not generate a message during slot x-1
	// as it will never be included, hence -1.
	return bc.GetEpochFirstSlot(lastEpoch+1) - 2
}

func (bc BeaconConfig) EstimatedCurrentEpochStartTime() time.Time {
	return bc.GetSlotStartTime(bc.GetEpochFirstSlot(bc.EstimatedCurrentEpoch()))
}
