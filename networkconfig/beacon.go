package networkconfig

import (
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"time"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// Beacon defines beacon network configuration. It is fetched from the consensus client during the node runtime.
type Beacon struct {
	Name                                 string
	SlotDuration                         time.Duration
	SlotsPerEpoch                        uint64
	EpochsPerSyncCommitteePeriod         uint64
	SyncCommitteeSize                    uint64
	SyncCommitteeSubnetCount             uint64
	TargetAggregatorsPerSyncSubcommittee uint64
	TargetAggregatorsPerCommittee        uint64
	IntervalsPerSlot                     uint64
	GenesisForkVersion                   phase0.Version
	GenesisTime                          time.Time
	GenesisValidatorsRoot                phase0.Root
	Forks                                map[spec.DataVersion]phase0.Fork
}

func (b *Beacon) String() string {
	marshaled, err := json.Marshal(b)
	if err != nil {
		panic(err)
	}

	return string(marshaled)
}

// SlotStartTime returns the start time for the given slot
func (b *Beacon) SlotStartTime(slot phase0.Slot) time.Time {
	if slot > math.MaxInt64 {
		panic(fmt.Sprintf("slot %d out of range", slot))
	}
	durationSinceGenesisStart := time.Duration(slot) * b.SlotDuration // #nosec G115: slot cannot exceed math.MaxInt64
	start := b.GenesisTime.Add(durationSinceGenesisStart)
	return start
}

// SlotEndTime returns the end time for the given slot
func (b *Beacon) SlotEndTime(slot phase0.Slot) time.Time {
	return b.SlotStartTime(slot + 1)
}

// EstimatedCurrentSlot returns the estimation of the current slot
func (b *Beacon) EstimatedCurrentSlot() phase0.Slot {
	return b.EstimatedSlotAtTime(time.Now())
}

// EstimatedSlotAtTime estimates slot at the given time
func (b *Beacon) EstimatedSlotAtTime(time time.Time) phase0.Slot {
	if time.Before(b.GenesisTime) {
		panic(fmt.Sprintf("time %v is before genesis time %v", time, b.GenesisTime))
	}
	timeAfterGenesis := time.Sub(b.GenesisTime)
	return phase0.Slot(timeAfterGenesis / b.SlotDuration) // #nosec G115: genesis can't be negative
}

// EstimatedCurrentEpoch estimates the current epoch
// https://github.com/ethereum/eth2.0-specs/blob/dev/specs/phase0/beacon-chain.md#compute_start_slot_at_epoch
func (b *Beacon) EstimatedCurrentEpoch() phase0.Epoch {
	return b.EstimatedEpochAtSlot(b.EstimatedCurrentSlot())
}

// EstimatedEpochAtSlot estimates epoch at the given slot
func (b *Beacon) EstimatedEpochAtSlot(slot phase0.Slot) phase0.Epoch {
	return phase0.Epoch(uint64(slot) / b.SlotsPerEpoch)
}

// IsFirstSlotOfEpoch estimates epoch at the given slot
func (b *Beacon) IsFirstSlotOfEpoch(slot phase0.Slot) bool {
	return uint64(slot)%b.SlotsPerEpoch == 0
}

// EstimatedSyncCommitteePeriodAtEpoch estimates the current sync committee period at the given Epoch
func (b *Beacon) EstimatedSyncCommitteePeriodAtEpoch(epoch phase0.Epoch) uint64 {
	return uint64(epoch) / b.EpochsPerSyncCommitteePeriod
}

// FirstEpochOfSyncPeriod calculates the first epoch of the given sync period.
func (b *Beacon) FirstEpochOfSyncPeriod(period uint64) phase0.Epoch {
	return phase0.Epoch(period * b.EpochsPerSyncCommitteePeriod)
}

// LastSlotOfSyncPeriod calculates the first epoch of the given sync period.
func (b *Beacon) LastSlotOfSyncPeriod(period uint64) phase0.Slot {
	lastEpoch := b.FirstEpochOfSyncPeriod(period+1) - 1
	// If we are in the sync committee that ends at slot x we do not generate a message during slot x-1
	// as it will never be included, hence -1.
	return b.FirstSlotAtEpoch(lastEpoch+1) - 2
}

func (b *Beacon) FirstSlotAtEpoch(epoch phase0.Epoch) phase0.Slot {
	return phase0.Slot(uint64(epoch) * b.SlotsPerEpoch)
}

func (b *Beacon) EpochStartTime(epoch phase0.Epoch) time.Time {
	firstSlot := b.FirstSlotAtEpoch(epoch)
	t := b.EstimatedTimeAtSlot(firstSlot)
	return t
}

func (b *Beacon) EstimatedTimeAtSlot(slot phase0.Slot) time.Time {
	if slot > math.MaxInt64 {
		panic(fmt.Sprintf("slot %d out of range", slot))
	}
	d := time.Duration(slot) * b.SlotDuration // #nosec G115: slot cannot exceed math.MaxInt64
	return b.GenesisTime.Add(d)
}

func (b *Beacon) IntervalDuration() time.Duration {
	if b.IntervalsPerSlot > math.MaxInt64 {
		panic("intervals per slot out of range")
	}
	return b.SlotDuration / time.Duration(b.IntervalsPerSlot) // #nosec G115: intervals per slot cannot exceed math.MaxInt64
}

func (b *Beacon) EpochDuration() time.Duration {
	if b.SlotsPerEpoch > math.MaxInt64 {
		panic("slots per epoch out of range")
	}
	return b.SlotDuration * time.Duration(b.SlotsPerEpoch) // #nosec G115: slot cannot exceed math.MaxInt64
}

func (b *Beacon) BeaconForkAtEpoch(epoch phase0.Epoch) (spec.DataVersion, *phase0.Fork) {
	versions := []spec.DataVersion{
		spec.DataVersionPhase0,
		spec.DataVersionAltair,
		spec.DataVersionBellatrix,
		spec.DataVersionCapella,
		spec.DataVersionDeneb,
		spec.DataVersionElectra,
	}

	for i, v := range versions {
		if epoch < b.Forks[v].Epoch {
			if i == 0 {
				panic("epoch before genesis")
			}

			version := versions[i-1]
			fork := b.Forks[version]
			return version, &fork
		}
	}

	version := versions[len(versions)-1]
	fork := b.Forks[version]
	return version, &fork
}

func (b *Beacon) AssertSame(other *Beacon) error {
	if b.Name != other.Name {
		return fmt.Errorf("different Name")
	}
	if b.SlotDuration != other.SlotDuration {
		return fmt.Errorf("different SlotDuration")
	}
	if b.SlotsPerEpoch != other.SlotsPerEpoch {
		return fmt.Errorf("different SlotsPerEpoch")
	}
	if b.EpochsPerSyncCommitteePeriod != other.EpochsPerSyncCommitteePeriod {
		return fmt.Errorf("different EpochsPerSyncCommitteePeriod")
	}
	if b.SyncCommitteeSize != other.SyncCommitteeSize {
		return fmt.Errorf("different SyncCommitteeSize")
	}
	if b.SyncCommitteeSubnetCount != other.SyncCommitteeSubnetCount {
		return fmt.Errorf("different SyncCommitteeSubnetCount")
	}
	if b.TargetAggregatorsPerSyncSubcommittee != other.TargetAggregatorsPerSyncSubcommittee {
		return fmt.Errorf("different TargetAggregatorsPerSyncSubcommittee")
	}
	if b.TargetAggregatorsPerCommittee != other.TargetAggregatorsPerCommittee {
		return fmt.Errorf("different TargetAggregatorsPerCommittee")
	}
	if b.IntervalsPerSlot != other.IntervalsPerSlot {
		return fmt.Errorf("different IntervalsPerSlot")
	}
	if b.GenesisForkVersion != other.GenesisForkVersion {
		return fmt.Errorf("different GenesisForkVersion")
	}
	if b.GenesisTime != other.GenesisTime {
		return fmt.Errorf("different GenesisTime")
	}
	if b.GenesisValidatorsRoot != other.GenesisValidatorsRoot {
		return fmt.Errorf("different GenesisValidatorsRoot")
	}
	if !maps.Equal(b.Forks, other.Forks) {
		return fmt.Errorf("different Forks")
	}

	return nil
}
