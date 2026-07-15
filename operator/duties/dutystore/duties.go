package dutystore

import (
	"sync"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

type Duty interface {
	eth2apiv1.AttesterDuty | eth2apiv1.ProposerDuty | gloas.PTCDuty
}

type StoreDuty[D Duty] struct {
	Slot           phase0.Slot
	ValidatorIndex phase0.ValidatorIndex
	Duty           *D
	InCommittee    bool
}

type Duties[D Duty] struct {
	mu sync.RWMutex
	m  map[phase0.Epoch]map[phase0.Slot]map[phase0.ValidatorIndex]StoreDuty[D]
	// stale flags epochs whose cached duties were fetched before the latest validator-set change.
	// The data keeps being served — only freshness-aware checks consult the flag via IsEpochStale —
	// and Set (a completed refetch) clears it.
	stale map[phase0.Epoch]struct{}
}

func NewDuties[D Duty]() *Duties[D] {
	return &Duties[D]{
		m:     make(map[phase0.Epoch]map[phase0.Slot]map[phase0.ValidatorIndex]StoreDuty[D]),
		stale: make(map[phase0.Epoch]struct{}),
	}
}

func (d *Duties[D]) CommitteeSlotDuties(epoch phase0.Epoch, slot phase0.Slot) []*D {
	d.mu.RLock()
	defer d.mu.RUnlock()

	slotMap, ok := d.m[epoch]
	if !ok {
		return nil
	}

	descriptorMap, ok := slotMap[slot]
	if !ok {
		return nil
	}

	var duties []*D
	for _, descriptor := range descriptorMap {
		if descriptor.InCommittee {
			duties = append(duties, descriptor.Duty)
		}
	}

	return duties
}

func (d *Duties[D]) ValidatorDuty(epoch phase0.Epoch, slot phase0.Slot, validatorIndex phase0.ValidatorIndex) *D {
	d.mu.RLock()
	defer d.mu.RUnlock()

	slotMap, ok := d.m[epoch]
	if !ok {
		return nil
	}

	descriptorMap, ok := slotMap[slot]
	if !ok {
		return nil
	}

	descriptor, ok := descriptorMap[validatorIndex]
	if !ok {
		return nil
	}

	return descriptor.Duty
}

// SlotIndices returns all validator indices that have a duty at the given (epoch, slot).
// It does not filter by InCommittee.
func (d *Duties[D]) SlotIndices(epoch phase0.Epoch, slot phase0.Slot) []phase0.ValidatorIndex {
	d.mu.RLock()
	defer d.mu.RUnlock()

	slotMap, ok := d.m[epoch]
	if !ok {
		return nil
	}
	descriptorMap, ok := slotMap[slot]
	if !ok {
		return nil
	}
	out := make([]phase0.ValidatorIndex, 0, len(descriptorMap))
	for idx := range descriptorMap {
		out = append(out, idx)
	}
	return out
}

func (d *Duties[D]) Set(epoch phase0.Epoch, duties []StoreDuty[D]) {
	mapped := make(map[phase0.Slot]map[phase0.ValidatorIndex]StoreDuty[D])
	for _, duty := range duties {
		if _, ok := mapped[duty.Slot]; !ok {
			mapped[duty.Slot] = make(map[phase0.ValidatorIndex]StoreDuty[D])
		}
		mapped[duty.Slot][duty.ValidatorIndex] = duty
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	d.m[epoch] = mapped
	delete(d.stale, epoch) // a completed fetch is fresh by definition
}

func (d *Duties[D]) EraseEpochData(epoch phase0.Epoch) {
	d.mu.Lock()
	defer d.mu.Unlock()

	delete(d.m, epoch)
	delete(d.stale, epoch)
}

// EraseBefore drops every cached epoch earlier than the given one, bounding the per-epoch cache.
func (d *Duties[D]) EraseBefore(epoch phase0.Epoch) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for cached := range d.m {
		if cached < epoch {
			delete(d.m, cached)
		}
	}
	for cached := range d.stale {
		if cached < epoch {
			delete(d.stale, cached)
		}
	}
}

// Clear drops every cached epoch. Used when a refresh must replace the whole cache rather than
// merge into it — e.g. PTC duties after a reorg or validator-set change (SIP #94 §3).
func (d *Duties[D]) Clear() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.m = make(map[phase0.Epoch]map[phase0.Slot]map[phase0.ValidatorIndex]StoreDuty[D])
	d.stale = make(map[phase0.Epoch]struct{})
}

func (d *Duties[D]) IsEpochSet(epoch phase0.Epoch) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()

	_, exists := d.m[epoch]
	return exists
}

// MarkEpochsStale flags the epochs' cached duties as fetched before the latest validator-set change.
// The data keeps being served (checks that must always enforce assignment still do), but
// freshness-aware duty-existence checks — §5 proposer preferences and the §6 self-build envelope —
// treat a stale epoch like a not-yet-fetched one until a refetch (Set) replaces it: a view predating
// a just-added validator must not permanently reject that validator's honest one-shot messages.
func (d *Duties[D]) MarkEpochsStale(epochs ...phase0.Epoch) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, epoch := range epochs {
		d.stale[epoch] = struct{}{}
	}
}

// IsEpochStale reports whether the epoch's cached duties predate the latest validator-set change.
func (d *Duties[D]) IsEpochStale(epoch phase0.Epoch) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()

	_, stale := d.stale[epoch]
	return stale
}
