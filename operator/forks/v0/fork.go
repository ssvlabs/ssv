package v0

import (
	ibftControllerFork "github.com/bloxapp/ssv/ibft/controller/forks"
	ibftControllerForkV0 "github.com/bloxapp/ssv/ibft/controller/forks/v0"
	networkForks "github.com/bloxapp/ssv/network/forks"
	networkForkV1 "github.com/bloxapp/ssv/network/forks/v1"
	"github.com/bloxapp/ssv/operator/forks"
	"github.com/bloxapp/ssv/utils/threadsafe"
)

// ForkV0 is the genesis operator fork
type ForkV0 struct {
	ibftFork    ibftControllerFork.Fork
	networkFork networkForks.Fork
	//storageFork storageForks.Fork
	currentSlot *threadsafe.SafeUint64
}

// New returns a new ForkV0 instance
func New() forks.Fork {
	return &ForkV0{
		ibftFork:    ibftControllerForkV0.New(),
		networkFork: networkForkV1.New(forks.NetworkV1ForkSlot),
		currentSlot: threadsafe.Uint64(0),
	}
}

// CurrentSlot returns the current known slot by the fork
func (v0 *ForkV0) CurrentSlot() uint64 {
	return v0.currentSlot.Get()
}

// SlotTick implementation
func (v0 *ForkV0) SlotTick(slot uint64) {
	v0.currentSlot.Set(slot)
	v0.ibftFork.SlotTick(slot)
	v0.networkFork.SlotTick(slot)
	//v0.storageFork.SlotTick(slot)
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
//func (v0 *ForkV0) StorageFork() storageForks.Fork {
//	return v0.storageFork
//}
