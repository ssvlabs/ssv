package dutystore

import (
	"sync"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

type SyncCommitteeDuties struct {
	mu sync.RWMutex
	m  map[uint64]map[phase0.ValidatorIndex]dutyDescriptor[eth2apiv1.SyncCommitteeDuty]
}

func NewSyncCommitteeDuties() *SyncCommitteeDuties {
	return &SyncCommitteeDuties{
		m: make(map[uint64]map[phase0.ValidatorIndex]dutyDescriptor[eth2apiv1.SyncCommitteeDuty]),
	}
}

func (d *SyncCommitteeDuties) CommitteePeriodDuties(period uint64) []*eth2apiv1.SyncCommitteeDuty {
	d.mu.RLock()
	defer d.mu.RUnlock()

	descriptorMap, ok := d.m[period]
	if !ok {
		return nil
	}

	var duties []*eth2apiv1.SyncCommitteeDuty
	for _, descriptor := range descriptorMap {
		if descriptor.inCommittee {
			duties = append(duties, descriptor.duty)
		}
	}

	return duties
}

func (d *SyncCommitteeDuties) Duty(period uint64, validatorIndex phase0.ValidatorIndex) *eth2apiv1.SyncCommitteeDuty {
	d.mu.RLock()
	defer d.mu.RUnlock()

	duties, ok := d.m[period]
	if !ok {
		return nil
	}

	descriptor, ok := duties[validatorIndex]
	if !ok {
		return nil
	}

	return descriptor.duty
}

func (d *SyncCommitteeDuties) Add(period uint64, validatorIndex phase0.ValidatorIndex, duty *eth2apiv1.SyncCommitteeDuty, inCommittee bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, ok := d.m[period]; !ok {
		d.m[period] = make(map[phase0.ValidatorIndex]dutyDescriptor[eth2apiv1.SyncCommitteeDuty])
	}

	d.m[period][validatorIndex] = dutyDescriptor[eth2apiv1.SyncCommitteeDuty]{
		duty:        duty,
		inCommittee: inCommittee,
	}
}

func (d *SyncCommitteeDuties) Reset(period uint64) {
	d.mu.Lock()
	defer d.mu.Unlock()

	delete(d.m, period)
}
