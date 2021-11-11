package v0

import (
	ibftControllerFork "github.com/bloxapp/ssv/ibft/controller/forks"
	ibftControllerForkV0 "github.com/bloxapp/ssv/ibft/controller/forks/v0"
	networkForks "github.com/bloxapp/ssv/network/forks"
	networkForkV0 "github.com/bloxapp/ssv/network/forks/v0"
	"github.com/bloxapp/ssv/operator/forks"
	storageForks "github.com/bloxapp/ssv/storage/forks"
)

// ForkV0 is the genesis operator fork
type ForkV0 struct {
	ibftForks   []ibftControllerFork.Fork
	networkFork networkForks.Fork
	storageFork storageForks.Fork
}

// New returns a new ForkV0 instance
func New() forks.Fork {
	return &ForkV0{
		ibftForks:   make([]ibftControllerFork.Fork, 0),
		networkFork: networkForkV0.New(),
	}
}

// SlotTick implementation
func (v0 *ForkV0) SlotTick(slot uint64) {
	v0.networkFork.SlotTick(slot)
	v0.storageFork.SlotTick(slot)
	for _, f := range v0.ibftForks {
		f.SlotTick(slot)
	}
}

// IBFTControllerFork returns ibft controller fork
func (v0 *ForkV0) NewIBFTControllerFork() ibftControllerFork.Fork {
	newFork := ibftControllerForkV0.New()
	v0.ibftForks = append(v0.ibftForks, newFork)
	return newFork
}

// NetworkFork returns network fork
func (v0 *ForkV0) NetworkFork() networkForks.Fork {
	return v0.networkFork
}

// StorageFork returns storage fork
func (v0 *ForkV0) StorageFork() storageForks.Fork {
	return v0.storageFork
}
