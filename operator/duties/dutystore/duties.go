package dutystore

import (
	"sync"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

type Duty interface {
	eth2apiv1.AttesterDuty | eth2apiv1.ProposerDuty | eth2apiv1.SyncCommitteeDuty
}

type dutyDescriptor[D Duty] struct {
	duty        *D
	inCommittee bool
}

type Duties[D Duty] struct {
	mu sync.RWMutex
	m  map[phase0.Epoch]map[phase0.Slot]map[phase0.ValidatorIndex]dutyDescriptor[D]
}

func NewDuties[D Duty]() *Duties[D] {
	return &Duties[D]{
		m: make(map[phase0.Epoch]map[phase0.Slot]map[phase0.ValidatorIndex]dutyDescriptor[D]),
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
		if descriptor.inCommittee {
			duties = append(duties, descriptor.duty)
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

	return descriptor.duty
}

func (d *Duties[D]) Add(epoch phase0.Epoch, slot phase0.Slot, validatorIndex phase0.ValidatorIndex, duty *D, inCommittee bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, ok := d.m[epoch]; !ok {
		d.m[epoch] = make(map[phase0.Slot]map[phase0.ValidatorIndex]dutyDescriptor[D])
	}
	if _, ok := d.m[epoch][slot]; !ok {
		d.m[epoch][slot] = make(map[phase0.ValidatorIndex]dutyDescriptor[D])
	}
	d.m[epoch][slot][validatorIndex] = dutyDescriptor[D]{
		duty:        duty,
		inCommittee: inCommittee,
	}
}

func (d *Duties[D]) ResetEpoch(epoch phase0.Epoch) {
	d.mu.Lock()
	defer d.mu.Unlock()

	delete(d.m, epoch)
}
