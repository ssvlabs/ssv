package networkconfig

import (
	"fmt"
	"maps"
	"math"
	"time"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/sanity-io/litter"
)

//go:generate go tool -modfile=../tool.mod mockgen -package=networkconfig -destination=./beacon_mock.go -source=./beacon.go

type Beacon interface {
	NetworkName() string
	SlotStartTime(slot phase0.Slot) time.Time
	SlotEndTime(slot phase0.Slot) time.Time
	EstimatedCurrentSlot() phase0.Slot
	EstimatedSlotAtTime(time time.Time) phase0.Slot
	EstimatedCurrentEpoch() phase0.Epoch
	EstimatedEpochAtSlot(slot phase0.Slot) phase0.Epoch
	IsFirstSlotOfEpoch(slot phase0.Slot) bool
	EpochFirstSlot(epoch phase0.Epoch) phase0.Slot
	EpochsPerSyncCommitteePeriod() uint64
	EstimatedSyncCommitteePeriodAtEpoch(epoch phase0.Epoch) uint64
	FirstEpochOfSyncPeriod(period uint64) phase0.Epoch
	LastSlotOfSyncPeriod(period uint64) phase0.Slot
	FirstSlotAtEpoch(epoch phase0.Epoch) phase0.Slot
	EpochStartTime(epoch phase0.Epoch) time.Time
	EstimatedTimeAtSlot(slot phase0.Slot) time.Time
	IntervalDuration() time.Duration
	EpochDuration() time.Duration
	SlotDuration() time.Duration
	SlotsPerEpoch() uint64
	GenesisTime() time.Time
	SyncCommitteeSize() uint64
	GenesisValidatorsRoot() phase0.Root
	ForkAtEpoch(epoch phase0.Epoch) (spec.DataVersion, phase0.Fork)
}

type BeaconConfig struct {
	networkName                          string
	slotDuration                         time.Duration
	slotsPerEpoch                        uint64
	epochsPerSyncCommitteePeriod         uint64
	syncCommitteeSize                    uint64
	syncCommitteeSubnetCount             uint64
	targetAggregatorsPerSyncSubcommittee uint64
	targetAggregatorsPerCommittee        uint64
	intervalsPerSlot                     uint64
	genesisForkVersion                   phase0.Version
	genesisTime                          time.Time
	genesisValidatorsRoot                phase0.Root
	forks                                map[spec.DataVersion]phase0.Fork
}

func NewBeaconConfig(
	networkName string,
	slotDuration time.Duration,
	slotsPerEpoch uint64,
	epochsPerSyncCommitteePeriod uint64,
	syncCommitteeSize uint64,
	syncCommitteeSubnetCount uint64,
	targetAggregatorsPerSyncSubcommittee uint64,
	targetAggregatorsPerCommittee uint64,
	intervalsPerSlot uint64,
	genesisForkVersion phase0.Version,
	genesisTime time.Time,
	genesisValidatorsRoot phase0.Root,
	forkData map[spec.DataVersion]phase0.Fork,
) *BeaconConfig {
	return &BeaconConfig{
		networkName:                          networkName,
		slotDuration:                         slotDuration,
		slotsPerEpoch:                        slotsPerEpoch,
		epochsPerSyncCommitteePeriod:         epochsPerSyncCommitteePeriod,
		syncCommitteeSize:                    syncCommitteeSize,
		syncCommitteeSubnetCount:             syncCommitteeSubnetCount,
		targetAggregatorsPerSyncSubcommittee: targetAggregatorsPerSyncSubcommittee,
		targetAggregatorsPerCommittee:        targetAggregatorsPerCommittee,
		intervalsPerSlot:                     intervalsPerSlot,
		genesisForkVersion:                   genesisForkVersion,
		genesisTime:                          genesisTime,
		genesisValidatorsRoot:                genesisValidatorsRoot,
		forks:                                forkData,
	}
}

// String implements Stringer interface.
func (b *BeaconConfig) String() string {
	return litter.Options{HidePrivateFields: false}.Sdump(b)
}

// SlotStartTime returns the start time for the given slot
func (b *BeaconConfig) SlotStartTime(slot phase0.Slot) time.Time {
	if slot > math.MaxInt64 {
		panic(fmt.Sprintf("slot %d out of range", slot))
	}
	durationSinceGenesisStart := time.Duration(slot) * b.slotDuration // #nosec G115: slot cannot exceed math.MaxInt64
	start := b.genesisTime.Add(durationSinceGenesisStart)
	return start
}

// SlotEndTime returns the end time for the given slot
func (b *BeaconConfig) SlotEndTime(slot phase0.Slot) time.Time {
	return b.SlotStartTime(slot + 1)
}

// EstimatedCurrentSlot returns the estimation of the current slot
func (b *BeaconConfig) EstimatedCurrentSlot() phase0.Slot {
	return b.EstimatedSlotAtTime(time.Now())
}

// EstimatedSlotAtTime estimates slot at the given time
func (b *BeaconConfig) EstimatedSlotAtTime(time time.Time) phase0.Slot {
	if time.Before(b.genesisTime) {
		panic(fmt.Sprintf("time %v is before genesis time %v", time, b.genesisTime))
	}
	timeAfterGenesis := time.Sub(b.genesisTime)
	return phase0.Slot(timeAfterGenesis / b.slotDuration) // #nosec G115: genesis can't be negative
}

// EstimatedCurrentEpoch estimates the current epoch
// https://github.com/ethereum/eth2.0-specs/blob/dev/specs/phase0/beacon-chain.md#compute_start_slot_at_epoch
func (b *BeaconConfig) EstimatedCurrentEpoch() phase0.Epoch {
	return b.EstimatedEpochAtSlot(b.EstimatedCurrentSlot())
}

// EstimatedEpochAtSlot estimates epoch at the given slot
func (b *BeaconConfig) EstimatedEpochAtSlot(slot phase0.Slot) phase0.Epoch {
	return phase0.Epoch(uint64(slot) / b.slotsPerEpoch)
}

// IsFirstSlotOfEpoch estimates epoch at the given slot
func (b *BeaconConfig) IsFirstSlotOfEpoch(slot phase0.Slot) bool {
	return uint64(slot)%b.slotsPerEpoch == 0
}

// EpochFirstSlot returns the beacon node first slot in epoch
func (b *BeaconConfig) EpochFirstSlot(epoch phase0.Epoch) phase0.Slot {
	return phase0.Slot(uint64(epoch) * b.slotsPerEpoch)
}

// EpochsPerSyncCommitteePeriod returns the number of epochs per sync committee period.
func (b *BeaconConfig) EpochsPerSyncCommitteePeriod() uint64 {
	return b.epochsPerSyncCommitteePeriod
}

// EstimatedSyncCommitteePeriodAtEpoch estimates the current sync committee period at the given Epoch
func (b *BeaconConfig) EstimatedSyncCommitteePeriodAtEpoch(epoch phase0.Epoch) uint64 {
	return uint64(epoch) / b.EpochsPerSyncCommitteePeriod()
}

// FirstEpochOfSyncPeriod calculates the first epoch of the given sync period.
func (b *BeaconConfig) FirstEpochOfSyncPeriod(period uint64) phase0.Epoch {
	return phase0.Epoch(period * b.EpochsPerSyncCommitteePeriod())
}

// LastSlotOfSyncPeriod calculates the first epoch of the given sync period.
func (b *BeaconConfig) LastSlotOfSyncPeriod(period uint64) phase0.Slot {
	lastEpoch := b.FirstEpochOfSyncPeriod(period+1) - 1
	// If we are in the sync committee that ends at slot x we do not generate a message during slot x-1
	// as it will never be included, hence -1.
	return b.EpochFirstSlot(lastEpoch+1) - 2
}

func (b *BeaconConfig) FirstSlotAtEpoch(epoch phase0.Epoch) phase0.Slot {
	return phase0.Slot(uint64(epoch) * b.slotsPerEpoch)
}

func (b *BeaconConfig) EpochStartTime(epoch phase0.Epoch) time.Time {
	firstSlot := b.FirstSlotAtEpoch(epoch)
	t := b.EstimatedTimeAtSlot(firstSlot)
	return t
}

func (b *BeaconConfig) EstimatedTimeAtSlot(slot phase0.Slot) time.Time {
	if slot > math.MaxInt64 {
		panic(fmt.Sprintf("slot %d out of range", slot))
	}
	d := time.Duration(slot) * b.slotDuration // #nosec G115: slot cannot exceed math.MaxInt64
	return b.genesisTime.Add(d)
}

func (b *BeaconConfig) IntervalDuration() time.Duration {
	if b.intervalsPerSlot > math.MaxInt64 {
		panic("intervals per slot out of range")
	}
	return b.slotDuration / time.Duration(b.intervalsPerSlot) // #nosec G115: intervals per slot cannot exceed math.MaxInt64
}

func (b *BeaconConfig) EpochDuration() time.Duration {
	if b.slotsPerEpoch > math.MaxInt64 {
		panic("slots per epoch out of range")
	}
	return b.slotDuration * time.Duration(b.slotsPerEpoch) // #nosec G115: slot cannot exceed math.MaxInt64
}

func (b *BeaconConfig) SlotDuration() time.Duration {
	return b.slotDuration
}

func (b *BeaconConfig) SlotsPerEpoch() uint64 {
	return b.slotsPerEpoch
}

func (b *BeaconConfig) GenesisForkVersion() phase0.Version {
	return b.genesisForkVersion
}

func (b *BeaconConfig) GenesisTime() time.Time {
	return b.genesisTime
}

func (b *BeaconConfig) SyncCommitteeSize() uint64 {
	return b.syncCommitteeSize
}

func (b *BeaconConfig) SyncCommitteeSubnetCount() uint64 {
	return b.syncCommitteeSubnetCount
}

func (b *BeaconConfig) TargetAggregatorsPerSyncSubcommittee() uint64 {
	return b.targetAggregatorsPerSyncSubcommittee
}

func (b *BeaconConfig) TargetAggregatorsPerCommittee() uint64 {
	return b.targetAggregatorsPerCommittee
}

func (b *BeaconConfig) GenesisValidatorsRoot() phase0.Root {
	return b.genesisValidatorsRoot
}

func (b *BeaconConfig) NetworkName() string {
	return b.networkName
}

func (b *BeaconConfig) Fork(version spec.DataVersion) phase0.Fork {
	return b.forks[version]
}

func (b *BeaconConfig) ForkAtEpoch(epoch phase0.Epoch) (spec.DataVersion, phase0.Fork) {
	versions := []spec.DataVersion{
		spec.DataVersionPhase0,
		spec.DataVersionAltair,
		spec.DataVersionBellatrix,
		spec.DataVersionCapella,
		spec.DataVersionDeneb,
		spec.DataVersionElectra,
	}

	for i, v := range versions {
		if epoch < b.forks[v].Epoch {
			if i == 0 {
				panic("epoch before genesis")
			}

			version := versions[i-1]
			return version, b.forks[version]
		}
	}

	version := versions[len(versions)-1]
	return version, b.forks[version]
}

func (b *BeaconConfig) AssertSame(other *BeaconConfig) error {
	if b.networkName != other.networkName {
		return fmt.Errorf("different NetworkName")
	}
	if b.slotDuration != other.slotDuration {
		return fmt.Errorf("different SlotDuration")
	}
	if b.slotsPerEpoch != other.slotsPerEpoch {
		return fmt.Errorf("different SlotsPerEpoch")
	}
	if b.epochsPerSyncCommitteePeriod != other.epochsPerSyncCommitteePeriod {
		return fmt.Errorf("different EpochsPerSyncCommitteePeriod")
	}
	if b.syncCommitteeSize != other.syncCommitteeSize {
		return fmt.Errorf("different SyncCommitteeSize")
	}
	if b.syncCommitteeSubnetCount != other.syncCommitteeSubnetCount {
		return fmt.Errorf("different SyncCommitteeSubnetCount")
	}
	if b.targetAggregatorsPerSyncSubcommittee != other.targetAggregatorsPerSyncSubcommittee {
		return fmt.Errorf("different TargetAggregatorsPerSyncSubcommittee")
	}
	if b.targetAggregatorsPerCommittee != other.targetAggregatorsPerCommittee {
		return fmt.Errorf("different TargetAggregatorsPerCommittee")
	}
	if b.intervalsPerSlot != other.intervalsPerSlot {
		return fmt.Errorf("different IntervalsPerSlot")
	}
	if b.genesisForkVersion != other.genesisForkVersion {
		return fmt.Errorf("different GenesisForkVersion")
	}
	if b.genesisTime != other.genesisTime {
		return fmt.Errorf("different GenesisTime")
	}
	if b.genesisValidatorsRoot != other.genesisValidatorsRoot {
		return fmt.Errorf("different GenesisValidatorsRoot")
	}

	if !maps.Equal(b.forks, other.forks) {
		return fmt.Errorf("different Forks")
	}
	return nil
}
