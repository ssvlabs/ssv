package v0

import (
	ibftControllerFork "github.com/bloxapp/ssv/ibft/controller/forks"
	ibftControllerForkV0 "github.com/bloxapp/ssv/ibft/controller/forks/v0"
	networkForks "github.com/bloxapp/ssv/network/forks"
	networkForkV0 "github.com/bloxapp/ssv/network/forks/v0"
	storageForks "github.com/bloxapp/ssv/storage/forks"
)

// ForkV0 is the genesis operator fork
type ForkV0 struct {
	ibftFork    ibftControllerFork.Fork
	networkFork networkForks.Fork
	storageFork storageForks.Fork
}

// New returns a new ForkV0 instance
func New() *ForkV0 {
	return &ForkV0{
		ibftFork:    ibftControllerForkV0.New(),
		networkFork: networkForkV0.New(),
	}
}

// SlotTick implementation
func (v0 *ForkV0) SlotTick(slot uint64) {
	v0.ibftFork.SlotTick(slot)
	v0.networkFork.SlotTick(slot)
	v0.storageFork.SlotTick(slot)
}

// IBFTControllerFork returns ibft controller fork
func (v0 *ForkV0) IBFTControllerFork() ibftControllerFork.Fork {
	return v0.ibftFork
}

// NetworkFork returns network fork
func (v0 *ForkV0) NetworkFork() networkForks.Fork {
	return v0.networkFork
}

// StorageFork returns storage fork
func (v0 *ForkV0) StorageFork() storageForks.Fork {
	return v0.storageFork
}
