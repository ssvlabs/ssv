package v0

import "github.com/bloxapp/ssv/network/forks"

// ForkV0 is the genesis version 0 implementation
type ForkV0 struct {
}

// New returns an instance of ForkV0
func New() forks.Fork {
	return &ForkV0{}
}

// SlotTick implementation
func (v0 *ForkV0) SlotTick(slot uint64) {

}
