package v1

import (
	"github.com/bloxapp/ssv/network/forks"
	"sync/atomic"
)

// forkedSlot represent the slot when fork happen
const forkedSlot uint64 = 30000000000 // TODO set as params

// state to know when fork start
const (
	stateBefore  uint64 = 0
	stateForking uint64 = 1
	stateAfter   uint64 = 2
)

// ForkV1 is the genesis version 0 implementation
type ForkV1 struct {
	state   uint64
	handler forks.OnFork
}

// New returns an instance of ForkV0
func New() forks.Fork {
	return &ForkV1{
		state: stateBefore,
	}
}

func (v1 *ForkV1) SetHandler(handler forks.OnFork) {
	v1.handler = handler
}

// SlotTick implementation
func (v1 *ForkV1) SlotTick(slot uint64) {
	if slot >= forkedSlot && !v1.IsForked() { // TODo check if can do this code with atomic func
		if v1.handler != nil {
			v1.handler()
		}
		atomic.StoreUint64(&v1.state, stateAfter)
	}
}

// IsForked return true if forked already started
func (v1 *ForkV1) IsForked() bool {
	return atomic.LoadUint64(&v1.state) == stateAfter
}
