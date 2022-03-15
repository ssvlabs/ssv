package v0

import (
	"github.com/bloxapp/eth2-key-manager/core"
	ibftControllerFork "github.com/bloxapp/ssv/ibft/controller/forks"
	ibftControllerForkV0 "github.com/bloxapp/ssv/ibft/controller/forks/v0"
	networkForks "github.com/bloxapp/ssv/network/forks"
	networkForkV1 "github.com/bloxapp/ssv/network/forks/v1"
	"github.com/bloxapp/ssv/operator/forks"
	storageForks "github.com/bloxapp/ssv/storage/forks"
	storageForksV0 "github.com/bloxapp/ssv/storage/forks/v0"
)

// ForkV0 is the genesis operator fork
type ForkV0 struct {
	network     core.Network
	ibftForks   []ibftControllerFork.Fork
	networkFork networkForks.Fork
	storageFork storageForks.Fork
}

// New returns a new ForkV0 instance
func New(network string) forks.Fork {
	return &ForkV0{
		network:     core.NetworkFromString(network),
		ibftForks:   make([]ibftControllerFork.Fork, 0),
		networkFork: networkForkV1.New(),
		storageFork: storageForksV0.New(),
	}
}

// Start all the necessary functionality
func (v0 *ForkV0) Start() {
	//	 update slot tick with current slot
	v0.SlotTick(v0.currentSlot())
}

// SlotTick implementation
func (v0 *ForkV0) SlotTick(slot uint64) {
	v0.networkFork.SlotTick(slot)
	v0.storageFork.SlotTick(slot)
	for _, f := range v0.ibftForks {
		f.SlotTick(slot)
	}
}

// NewIBFTControllerFork returns ibft controller fork
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

func (v0 *ForkV0) currentSlot() uint64 {
	return uint64(v0.network.EstimatedCurrentSlot())
}
