package networkconfig

import (
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"slices"
	"time"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// DataVersionGloas is the Gloas (ePBS) beacon data version — go-eth2-client's spec.DataVersionGloas,
// re-exported under the node's name for its call sites and for the ssvsigner mirror (ekm.GloasDataVersion,
// a separate module; a node-side test pins the two equal).
const DataVersionGloas = spec.DataVersionGloas

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

func (b *Beacon) NetworkName() string {
	return b.Name
}

func (b *Beacon) GenesisRoot() phase0.Root {
	return b.GenesisValidatorsRoot
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

// PayloadAttestationCutoff is the point 75% into the slot (PAYLOAD_ATTESTATION_DUE) at which a
// Gloas PTC member observes payload presence and runs its attestation.
func (b *Beacon) PayloadAttestationCutoff(slot phase0.Slot) time.Time {
	return b.SlotStartTime(slot).Add(b.SlotDuration * 3 / 4)
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

// EstimatedTimeIntoSlot returns the duration passed since EstimatedCurrentSlot.
func (b *Beacon) EstimatedTimeIntoSlot() time.Duration {
	return time.Since(b.TimeAtSlot(b.EstimatedCurrentSlot()))
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

// LastActionableSlotOfSyncPeriod calculates the last slot of the given sync period for which
// producing a sync committee message still makes sense. Note this is one slot before the period's
// literal last slot (a message produced during that final slot would never be included).
func (b *Beacon) LastActionableSlotOfSyncPeriod(period uint64) phase0.Slot {
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
	t := b.TimeAtSlot(firstSlot)
	return t
}

func (b *Beacon) TimeAtSlot(slot phase0.Slot) time.Time {
	if slot > math.MaxInt64 {
		panic(fmt.Sprintf("slot %d out of range", slot))
	}
	d := time.Duration(slot) * b.SlotDuration // #nosec G115: slot cannot exceed math.MaxInt64
	return b.GenesisTime.Add(d)
}

// IntervalDuration is the slot fraction that duty deadlines are multiples of: 1/3 of the slot before
// Gloas, 1/4 from Gloas on. ePBS retimes the deadlines to quarters — attestation/sync 1× (25%),
// aggregate/contribution 2× (50%), payload attestation 3× (75%); SIP #94 §1.
func (b *Beacon) IntervalDuration(slot phase0.Slot) time.Duration {
	intervalsPerSlot := 3
	if b.IsGloasAtSlot(slot) {
		intervalsPerSlot = 4
	}
	return b.SlotDuration / time.Duration(intervalsPerSlot)
}

func (b *Beacon) EpochDuration() time.Duration {
	if b.SlotsPerEpoch > math.MaxInt64 {
		panic("slots per epoch out of range")
	}
	return b.SlotDuration * time.Duration(b.SlotsPerEpoch) // #nosec G115: slot cannot exceed math.MaxInt64
}

// ForkAtEpoch returns the beacon fork active at the epoch, Gloas included, so fork-versioned values —
// attestations, the aggregator consensus data — stamp the slot's real fork (SIP #94 §2). Forks absent
// from the map are skipped: a Beacon without a Gloas entry still resolves to the latest fork it carries.
func (b *Beacon) ForkAtEpoch(epoch phase0.Epoch) (spec.DataVersion, *phase0.Fork) {
	versions := []spec.DataVersion{
		spec.DataVersionPhase0,
		spec.DataVersionAltair,
		spec.DataVersionBellatrix,
		spec.DataVersionCapella,
		spec.DataVersionDeneb,
		spec.DataVersionElectra,
		spec.DataVersionFulu,
		DataVersionGloas,
	}

	var (
		activeVersion spec.DataVersion
		activeFork    phase0.Fork
		hasActive     bool
	)
	for _, v := range versions {
		fork, ok := b.Forks[v]
		if !ok {
			continue
		}
		if epoch < fork.Epoch {
			if !hasActive {
				panic("epoch before genesis")
			}
			return activeVersion, &activeFork
		}
		activeVersion, activeFork, hasActive = v, fork, true
	}
	if !hasActive {
		panic("no forks configured")
	}
	return activeVersion, &activeFork
}

func (b *Beacon) ForkAtVersion(version spec.DataVersion) (phase0.Fork, bool) {
	fork, ok := b.Forks[version]
	return fork, ok
}

// IsGloas reports whether the beacon fork active at the given epoch is Gloas (ePBS).
// Returns false when there is no scheduled Gloas fork (absent from Forks or far-future),
// so it is safe on pre-Gloas networks and Beacon values without a Gloas entry.
func (b *Beacon) IsGloas(epoch phase0.Epoch) bool {
	fork, ok := b.Forks[DataVersionGloas]
	return ok && epoch >= fork.Epoch
}

// IsGloasAtSlot reports whether the Gloas (ePBS) fork is active at the given slot — the slot-keyed
// shorthand for IsGloas(EstimatedEpochAtSlot(slot)) used across the duty runners and validators.
func (b *Beacon) IsGloasAtSlot(slot phase0.Slot) bool {
	return b.IsGloas(b.EstimatedEpochAtSlot(slot))
}

// GloasForkEpoch returns the scheduled Gloas (ePBS) fork epoch and whether a Gloas fork is present in
// the schedule. An unscheduled far-future epoch is returned as-is; callers that gate on it (IsGloas,
// InGloasPriorWindow) treat it as never active via the epoch comparison.
func (b *Beacon) GloasForkEpoch() (phase0.Epoch, bool) {
	fork, ok := b.Forks[DataVersionGloas]
	return fork.Epoch, ok
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
	if b.GenesisForkVersion != other.GenesisForkVersion {
		return fmt.Errorf("different GenesisForkVersion")
	}
	if b.GenesisTime != other.GenesisTime {
		return fmt.Errorf("different GenesisTime")
	}
	if b.GenesisValidatorsRoot != other.GenesisValidatorsRoot {
		return fmt.Errorf("different GenesisValidatorsRoot")
	}
	if err := assertSameForks(b.Forks, other.Forks, b.EstimatedCurrentEpoch()); err != nil {
		return err
	}

	return nil
}

// FarFutureEpoch marks a fork the beacon node names but has not scheduled.
const FarFutureEpoch = phase0.Epoch(math.MaxUint64)

// ForkScheduleLagError reports two beacon configs that agree on the chain so far and disagree only about a
// fork still ahead of both: one schedules it and the other does not, or they schedule it at different epochs.
// Same genesis and same forks to date means the same chain, so such a disagreement is one client lagging its
// network's configuration — the normal state of a staggered client upgrade, until the lagging client is
// upgraded (or parts ways at the fork). Callers decide how loudly to say so; AssertSame returns it as is.
type ForkScheduleLagError struct {
	Version spec.DataVersion
	// Ours and Theirs are the two schedules; an unscheduled side carries FarFutureEpoch.
	Ours, Theirs phase0.Fork
}

func (e *ForkScheduleLagError) Error() string {
	return fmt.Sprintf("fork schedules differ ahead of the chain: %s is %s on one client and %s on the other",
		e.Version, DescribeForkSchedule(e.Ours), DescribeForkSchedule(e.Theirs))
}

// DescribeForkSchedule words a fork's schedule for logs and errors.
func DescribeForkSchedule(fork phase0.Fork) string {
	if fork.Epoch == FarFutureEpoch {
		return "not scheduled"
	}
	return fmt.Sprintf("scheduled at epoch %d (version %#x)", fork.Epoch, fork.CurrentVersion)
}

// assertSameForks compares two fork schedules. A fork unscheduled on both is the same whether a client names it
// (far-future epoch, any version) or omits it. Every fork active on either client at currentEpoch must be
// scheduled identically on both: they are on different chains otherwise. A disagreement confined to forks
// still ahead of both is a *ForkScheduleLagError.
func assertSameForks(ours, theirs map[spec.DataVersion]phase0.Fork, currentEpoch phase0.Epoch) error {
	schedule := func(forks map[spec.DataVersion]phase0.Fork, version spec.DataVersion) (phase0.Fork, bool) {
		fork, ok := forks[version]
		if !ok || fork.Epoch == FarFutureEpoch {
			return phase0.Fork{Epoch: FarFutureEpoch}, false
		}
		return fork, true
	}
	versions := make(map[spec.DataVersion]struct{}, len(ours)+len(theirs))
	for version := range ours {
		versions[version] = struct{}{}
	}
	for version := range theirs {
		versions[version] = struct{}{}
	}
	var lag *ForkScheduleLagError
	for _, version := range slices.Sorted(maps.Keys(versions)) {
		mine, mineScheduled := schedule(ours, version)
		other, otherScheduled := schedule(theirs, version)
		if (!mineScheduled && !otherScheduled) || mine == other {
			continue
		}
		active := (mineScheduled && mine.Epoch <= currentEpoch) || (otherScheduled && other.Epoch <= currentEpoch)
		if active {
			return fmt.Errorf("different Forks: %s is %s on one client and %s on the other",
				version, DescribeForkSchedule(mine), DescribeForkSchedule(other))
		}
		if lag == nil {
			lag = &ForkScheduleLagError{Version: version, Ours: mine, Theirs: other}
		}
	}
	if lag != nil {
		return lag
	}
	return nil
}
