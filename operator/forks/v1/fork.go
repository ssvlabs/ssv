package v1

import (
	ibftControllerFork "github.com/bloxapp/ssv/ibft/controller/forks"
	ibftControllerForkV0 "github.com/bloxapp/ssv/ibft/controller/forks/v0"
	networkForks "github.com/bloxapp/ssv/network/forks"
	networkForkV1 "github.com/bloxapp/ssv/network/forks/v1"
	"github.com/bloxapp/ssv/operator/forks"
	storageForks "github.com/bloxapp/ssv/storage/forks"
	storageForksV0 "github.com/bloxapp/ssv/storage/forks/v0"
)

// ForkV1 is the genesis operator fork
type ForkV1 struct {
	ibftForks   []ibftControllerFork.Fork
	networkFork networkForks.Fork
	storageFork storageForks.Fork
}

// New returns a new ForkV0 instance
func New() forks.Fork {
	return &ForkV1{
		ibftForks:   make([]ibftControllerFork.Fork, 0),
		networkFork: networkForkV1.New(),
		storageFork: storageForksV0.New(),
	}
}

// SlotTick implementation
func (v0 *ForkV1) SlotTick(slot uint64) {
	//v0.networkFork.SlotTick(slot)
	v0.storageFork.SlotTick(slot)
	for _, f := range v0.ibftForks {
		f.SlotTick(slot)
	}
}

// NewIBFTControllerFork returns ibft controller fork
func (v0 *ForkV1) NewIBFTControllerFork() ibftControllerFork.Fork {
	newFork := ibftControllerForkV0.New()
	v0.ibftForks = append(v0.ibftForks, newFork)
	return newFork
}

// NetworkFork returns network forker
func (v0 *ForkV1) NetworkFork() networkForks.Fork {
	return v0.networkFork
}

// StorageFork returns storage fork
func (v0 *ForkV1) StorageFork() storageForks.Fork {
	return v0.storageFork
}
