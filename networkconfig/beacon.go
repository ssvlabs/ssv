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
	GetEpochsPerSyncCommitteePeriod() uint64
	EstimatedSyncCommitteePeriodAtEpoch(epoch phase0.Epoch) uint64
	FirstEpochOfSyncPeriod(period uint64) phase0.Epoch
	LastSlotOfSyncPeriod(period uint64) phase0.Slot
	FirstSlotAtEpoch(epoch phase0.Epoch) phase0.Slot
	EpochStartTime(epoch phase0.Epoch) time.Time
	EstimatedTimeAtSlot(slot phase0.Slot) time.Time
	IntervalDuration() time.Duration
	EpochDuration() time.Duration
	GetSlotDuration() time.Duration
	GetSlotsPerEpoch() uint64
	GetGenesisTime() time.Time
	GetSyncCommitteeSize() uint64
	GetGenesisValidatorsRoot() phase0.Root
	GetNetworkName() string
	ForkAtEpoch(epoch phase0.Epoch) (spec.DataVersion, *phase0.Fork)
}

type BeaconConfig struct {
	NetworkName                          string
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

func (b BeaconConfig) String() string {
	marshaled, err := json.Marshal(b)
	if err != nil {
		panic(err)
	}

	return string(marshaled)
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
	return phase0.Epoch(uint64(slot) / b.SlotsPerEpoch)
}

// IsFirstSlotOfEpoch estimates epoch at the given slot
func (b BeaconConfig) IsFirstSlotOfEpoch(slot phase0.Slot) bool {
	return uint64(slot)%b.SlotsPerEpoch == 0
}

// GetEpochFirstSlot returns the beacon node first slot in epoch
func (b BeaconConfig) GetEpochFirstSlot(epoch phase0.Epoch) phase0.Slot {
	return phase0.Slot(uint64(epoch) * b.SlotsPerEpoch)
}

// GetEpochsPerSyncCommitteePeriod returns the number of epochs per sync committee period.
func (b BeaconConfig) GetEpochsPerSyncCommitteePeriod() uint64 {
	return b.EpochsPerSyncCommitteePeriod
}

// EstimatedSyncCommitteePeriodAtEpoch estimates the current sync committee period at the given Epoch
func (b BeaconConfig) EstimatedSyncCommitteePeriodAtEpoch(epoch phase0.Epoch) uint64 {
	return uint64(epoch) / b.GetEpochsPerSyncCommitteePeriod()
}

// FirstEpochOfSyncPeriod calculates the first epoch of the given sync period.
func (b BeaconConfig) FirstEpochOfSyncPeriod(period uint64) phase0.Epoch {
	return phase0.Epoch(period * b.GetEpochsPerSyncCommitteePeriod())
}

// LastSlotOfSyncPeriod calculates the first epoch of the given sync period.
func (b BeaconConfig) LastSlotOfSyncPeriod(period uint64) phase0.Slot {
	lastEpoch := b.FirstEpochOfSyncPeriod(period+1) - 1
	// If we are in the sync committee that ends at slot x we do not generate a message during slot x-1
	// as it will never be included, hence -1.
	return b.GetEpochFirstSlot(lastEpoch+1) - 2
}

func (b BeaconConfig) FirstSlotAtEpoch(epoch phase0.Epoch) phase0.Slot {
	return phase0.Slot(uint64(epoch) * b.SlotsPerEpoch)
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

func (b BeaconConfig) IntervalDuration() time.Duration {
	if b.IntervalsPerSlot > math.MaxInt64 {
		panic("intervals per slot out of range")
	}
	return b.SlotDuration / time.Duration(b.IntervalsPerSlot) // #nosec G115: intervals per slot cannot exceed math.MaxInt64
}

func (b BeaconConfig) EpochDuration() time.Duration {
	if b.SlotsPerEpoch > math.MaxInt64 {
		panic("slots per epoch out of range")
	}
	return b.SlotDuration * time.Duration(b.SlotsPerEpoch) // #nosec G115: slot cannot exceed math.MaxInt64
}

func (b BeaconConfig) GetSlotDuration() time.Duration {
	return b.SlotDuration
}

func (b BeaconConfig) GetSlotsPerEpoch() uint64 {
	return b.SlotsPerEpoch
}

func (b BeaconConfig) GetGenesisTime() time.Time {
	return b.GenesisTime
}

func (b BeaconConfig) GetSyncCommitteeSize() uint64 {
	return b.SyncCommitteeSize
}

func (b BeaconConfig) GetGenesisValidatorsRoot() phase0.Root {
	return b.GenesisValidatorsRoot
}

func (b BeaconConfig) GetNetworkName() string {
	return b.NetworkName
}

func (b BeaconConfig) ForkAtEpoch(epoch phase0.Epoch) (spec.DataVersion, *phase0.Fork) {
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

func (b BeaconConfig) AssertSame(other BeaconConfig) error {
	if b.NetworkName != other.NetworkName {
		return fmt.Errorf("different NetworkName")
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
