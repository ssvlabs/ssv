package dutystore

import (
	"sync"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

type StoreSyncCommitteeDuty struct {
	ValidatorIndex phase0.ValidatorIndex
	Duty           *eth2apiv1.SyncCommitteeDuty
	InCommittee    bool
}

type SyncCommitteeDuties struct {
	mu sync.RWMutex
	m  map[uint64]map[phase0.ValidatorIndex]StoreSyncCommitteeDuty
}

func NewSyncCommitteeDuties() *SyncCommitteeDuties {
	return &SyncCommitteeDuties{
		m: make(map[uint64]map[phase0.ValidatorIndex]StoreSyncCommitteeDuty),
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
		if descriptor.InCommittee {
			duties = append(duties, descriptor.Duty)
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

	return descriptor.Duty
}

func (d *SyncCommitteeDuties) Set(period uint64, duties []StoreSyncCommitteeDuty) {
	mapped := make(map[phase0.ValidatorIndex]StoreSyncCommitteeDuty)
	for _, duty := range duties {
		mapped[duty.ValidatorIndex] = duty
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	d.m[period] = mapped
}

func (d *SyncCommitteeDuties) Reset(period uint64) {
	d.mu.Lock()
	defer d.mu.Unlock()

	delete(d.m, period)
}
