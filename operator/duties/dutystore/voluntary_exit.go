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

func (ves *VoluntaryExitDuties) GetDutyCount(slot phase0.Slot, pk phase0.BLSPubKey) int {
	ves.mu.RLock()
	defer ves.mu.RUnlock()

	v, ok := ves.m[slot]
	if !ok {
		return 0
	}

	return v[pk]
}

func (ves *VoluntaryExitDuties) AddDuty(slot phase0.Slot, pk phase0.BLSPubKey) {
	ves.mu.Lock()
	defer ves.mu.Unlock()

	v, ok := ves.m[slot]
	if !ok {
		ves.m[slot] = map[phase0.BLSPubKey]int{
			pk: 1,
		}
	} else {
		v[pk]++
	}
}
