package networkconfig

import (
	"encoding/json"
	"math"
	"sync"
	"time"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

var (
	FarFutureEpoch phase0.Epoch = math.MaxUint64
)

type Beacon struct {
	NetworkName                          string
	CapellaForkVersion                   phase0.Version
	SlotDuration                         time.Duration
	SlotsPerEpoch                        phase0.Slot
	EpochsPerSyncCommitteePeriod         phase0.Epoch
	SyncCommitteeSize                    uint64
	SyncCommitteeSubnetCount             uint64
	TargetAggregatorsPerSyncSubcommittee uint64
	TargetAggregatorsPerCommittee        uint64
	IntervalsPerSlot                     uint64
	Genesis                              v1.Genesis
	ForkEpochs                           *ForkEpochs
}

func (b Beacon) Apply(other Beacon) bool {
	if !b.ForkEpochs.Apply(other.ForkEpochs) {
		return false
	}

	// Check all values except pointers using copy to make it less error-prone on field addition.
	tmp1 := b
	tmp2 := other
	tmp1.ForkEpochs = nil
	tmp2.ForkEpochs = nil

	return tmp1 == tmp2
}

func (b Beacon) String() string {
	marshaled, err := json.Marshal(b)
	if err != nil {
		panic(err)
	}

	return string(marshaled)
}

func (b Beacon) DataVersion(epoch phase0.Epoch) spec.DataVersion {
	b.ForkEpochs.upcomingForkMu.Lock()
	defer b.ForkEpochs.upcomingForkMu.Unlock()

	if epoch < b.ForkEpochs.Altair {
		return spec.DataVersionPhase0
	} else if epoch < b.ForkEpochs.Bellatrix {
		return spec.DataVersionAltair
	} else if epoch < b.ForkEpochs.Capella {
		return spec.DataVersionBellatrix
	} else if epoch < b.ForkEpochs.Deneb {
		return spec.DataVersionCapella
	} else if epoch < b.ForkEpochs.Electra {
		return spec.DataVersionDeneb
	}
	return spec.DataVersionElectra
}

func (b Beacon) GenesisTime() time.Time {
	return b.Genesis.GenesisTime
}

func (b Beacon) GenesisForkVersion() phase0.Version {
	return b.Genesis.GenesisForkVersion
}

// GetSlotStartTime returns the start time for the given slot
func (b Beacon) GetSlotStartTime(slot phase0.Slot) time.Time {
	if slot > math.MaxInt64 {
		panic("slot out of range")
	}
	durationSinceGenesisStart := time.Duration(slot) * b.SlotDuration // #nosec G115: slot cannot exceed math.MaxInt64
	return b.Genesis.GenesisTime.Add(durationSinceGenesisStart)
}

func (b Beacon) EstimatedTimeAtSlot(slot phase0.Slot) time.Time {
	if slot > math.MaxInt64 {
		panic("slot out of range")
	}
	d := time.Duration(slot) * b.SlotDuration // #nosec G115: slot cannot exceed math.MaxInt64
	return b.Genesis.GenesisTime.Add(d)
}

func (b Beacon) FirstSlotAtEpoch(epoch phase0.Epoch) phase0.Slot {
	return phase0.Slot(epoch) * b.SlotsPerEpoch
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
	genesis := b.GenesisTime()
	if time.Before(genesis) {
		return 0
	}
	timeAfterGenesis := time.Sub(genesis)
	return phase0.Slot(timeAfterGenesis / b.SlotDuration) // #nosec G115: genesis can't be negative
}

// EstimatedCurrentEpoch estimates the current epoch
// https://github.com/ethereum/eth2.0-specs/blob/dev/specs/phase0/beacon-chain.md#compute_start_slot_at_epoch
func (b Beacon) EstimatedCurrentEpoch() phase0.Epoch {
	return b.EstimatedEpochAtSlot(b.EstimatedCurrentSlot())
}

// EstimatedEpochAtSlot estimates epoch at the given slot
func (b Beacon) EstimatedEpochAtSlot(slot phase0.Slot) phase0.Epoch {
	return phase0.Epoch(slot / b.SlotsPerEpoch)
}

// IsFirstSlotOfEpoch estimates epoch at the given slot
func (b Beacon) IsFirstSlotOfEpoch(slot phase0.Slot) bool {
	return slot%b.SlotsPerEpoch == 0
}

// GetEpochFirstSlot returns the beacon node first slot in epoch
func (b Beacon) GetEpochFirstSlot(epoch phase0.Epoch) phase0.Slot {
	return phase0.Slot(epoch) * b.SlotsPerEpoch
}

// EstimatedSyncCommitteePeriodAtEpoch estimates the current sync committee period at the given Epoch
func (b Beacon) EstimatedSyncCommitteePeriodAtEpoch(epoch phase0.Epoch) uint64 {
	return uint64(epoch / b.EpochsPerSyncCommitteePeriod)
}

// FirstEpochOfSyncPeriod calculates the first epoch of the given sync period.
func (b Beacon) FirstEpochOfSyncPeriod(period uint64) phase0.Epoch {
	return phase0.Epoch(period) * b.EpochsPerSyncCommitteePeriod
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

func (b Beacon) IntervalDuration() time.Duration {
	if b.IntervalsPerSlot > math.MaxInt64 {
		panic("intervals per slot out of range")
	}
	return b.SlotDuration / time.Duration(b.IntervalsPerSlot) // #nosec G115: intervals per slot cannot exceed math.MaxInt64
}

// EpochDuration returns epoch duration
func (b Beacon) EpochDuration() time.Duration {
	slotsPerEpoch := b.SlotsPerEpoch
	if slotsPerEpoch > math.MaxInt64 {
		panic("slot out of range")
	}
	return b.SlotDuration * time.Duration(slotsPerEpoch) // #nosec G115: slot cannot exceed math.MaxInt64
}

func (b Beacon) SlotsPerPeriod() phase0.Slot {
	return phase0.Slot(b.EpochsPerSyncCommitteePeriod) * b.SlotsPerEpoch
}

type ForkEpochs struct {
	upcomingForkMu sync.Mutex
	Electra        phase0.Epoch // current upcoming fork, should be accessed with upcomingForkMu
	Deneb          phase0.Epoch
	Capella        phase0.Epoch
	Bellatrix      phase0.Epoch
	Altair         phase0.Epoch
}

func (f *ForkEpochs) Apply(other *ForkEpochs) bool {
	if f == nil || other == nil {
		return false
	}

	f.upcomingForkMu.Lock()
	defer f.upcomingForkMu.Unlock()

	// After Electra fork happens on all networks, Electra check should be replaced by Fulu check.
	if f.Electra == FarFutureEpoch && other.Electra != FarFutureEpoch {
		f.Electra = other.Electra
	}

	return f.Electra == other.Electra &&
		f.Deneb == other.Deneb &&
		f.Capella == other.Capella &&
		f.Bellatrix == other.Bellatrix &&
		f.Altair == other.Altair
}

func (f *ForkEpochs) ElectraForkEpoch() phase0.Epoch {
	f.upcomingForkMu.Lock()
	defer f.upcomingForkMu.Unlock()

	return f.Electra
}
