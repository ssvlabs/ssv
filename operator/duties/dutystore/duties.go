package dutystore

import (
	"sync"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

type Duty interface {
	eth2apiv1.AttesterDuty | eth2apiv1.ProposerDuty
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
}

func NewDuties[D Duty]() *Duties[D] {
	return &Duties[D]{
		m: make(map[phase0.Epoch]map[phase0.Slot]map[phase0.ValidatorIndex]StoreDuty[D]),
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
}

func (d *Duties[D]) ResetEpoch(epoch phase0.Epoch) {
	d.mu.Lock()
	defer d.mu.Unlock()

	delete(d.m, epoch)
}

func (d *Duties[D]) IsEpochSet(epoch phase0.Epoch) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()

	_, exists := d.m[epoch]
	return exists
}
