package dutystore

import (
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

type VoluntaryExitDuties struct {
	mu sync.RWMutex
	m  map[phase0.Slot]map[phase0.BLSPubKey]int
}

func NewVoluntaryExit() *VoluntaryExitDuties {
	return &VoluntaryExitDuties{
		m: make(map[phase0.Slot]map[phase0.BLSPubKey]int),
	}
}

func (d *VoluntaryExitDuties) GetDutyCount(slot phase0.Slot, pk phase0.BLSPubKey) int {
	d.mu.RLock()
	defer d.mu.RUnlock()

	v, ok := d.m[slot]
	if !ok {
		return 0
	}

	return v[pk]
}

func (d *VoluntaryExitDuties) AddDuty(slot phase0.Slot, pk phase0.BLSPubKey) {
	d.mu.Lock()
	defer d.mu.Unlock()

	v, ok := d.m[slot]
	if !ok {
		d.m[slot] = map[phase0.BLSPubKey]int{
			pk: 1,
		}
	} else {
		v[pk]++
	}
}

func (d *VoluntaryExitDuties) RemoveSlot(slot phase0.Slot) {
	d.mu.Lock()
	defer d.mu.Unlock()

	delete(d.m, slot)
}
