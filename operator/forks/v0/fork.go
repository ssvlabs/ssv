package v0

import (
	ibftControllerFork "github.com/bloxapp/ssv/ibft/controller/forks"
	ibftControllerForkV0 "github.com/bloxapp/ssv/ibft/controller/forks/v0"
	networkForks "github.com/bloxapp/ssv/network/forks"
	networkForkV1 "github.com/bloxapp/ssv/network/forks/v1"
	"github.com/bloxapp/ssv/operator/forks"
	storageForks "github.com/bloxapp/ssv/storage/forks"
	"github.com/bloxapp/ssv/utils/threadsafe"
	"go.uber.org/zap"
)

// ForkV0 is the genesis operator fork
type ForkV0 struct {
	ibftForks   []ibftControllerFork.Fork
	networkFork networkForks.Fork
	storageFork storageForks.Fork
	currentSlot *threadsafe.SafeUint64
	logger      *zap.Logger
}

// New returns a new ForkV0 instance
func New(logger *zap.Logger) forks.Fork {
	return &ForkV0{
		ibftForks:   make([]ibftControllerFork.Fork, 0),
		networkFork: networkForkV1.New(logger, forks.NetworkV1ForkSlot),
		logger:      logger,
		currentSlot: threadsafe.Uint64(0),
	}
}

// CurrentSlot implementation
func (v0 *ForkV0) CurrentSlot() uint64 {
	return v0.currentSlot.Get()
}

// SlotTick implementation
func (v0 *ForkV0) SlotTick(slot uint64) {
	v0.currentSlot.Set(slot)
	v0.networkFork.SlotTick(slot)
	//v0.storageFork.SlotTick(slot)
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
