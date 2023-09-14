package dutystorage

import (
	"sync"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"golang.org/x/exp/maps"
)

type SyncCommitteeDuties struct {
	mu sync.RWMutex
	m  map[uint64]map[phase0.ValidatorIndex]*eth2apiv1.SyncCommitteeDuty
}

func NewSyncCommittee() *SyncCommitteeDuties {
	return &SyncCommitteeDuties{
		m: make(map[uint64]map[phase0.ValidatorIndex]*eth2apiv1.SyncCommitteeDuty),
	}
}

func (d *SyncCommitteeDuties) PeriodDuties(period uint64) []*eth2apiv1.SyncCommitteeDuty {
	d.mu.RLock()
	defer d.mu.RUnlock()

	duties, ok := d.m[period]
	if !ok {
		return nil
	}

	return maps.Values(duties)
}

func (d *SyncCommitteeDuties) ValidatorDuty(period uint64, validatorIndex phase0.ValidatorIndex) *eth2apiv1.SyncCommitteeDuty {
	d.mu.RLock()
	defer d.mu.RUnlock()

	duties, ok := d.m[period]
	if !ok {
		return nil
	}

	duty, ok := duties[validatorIndex]
	if !ok {
		return nil
	}

	return duty
}

func (d *SyncCommitteeDuties) Add(period uint64, validatorIndex phase0.ValidatorIndex, duty *eth2apiv1.SyncCommitteeDuty) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, ok := d.m[period]; !ok {
		d.m[period] = make(map[phase0.ValidatorIndex]*eth2apiv1.SyncCommitteeDuty)
	}

	d.m[period][validatorIndex] = duty
}

func (d *SyncCommitteeDuties) ResetPeriod(period uint64) {
	d.mu.Lock()
	defer d.mu.Unlock()

	delete(d.m, period)
}
