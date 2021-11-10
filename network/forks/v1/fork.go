package v1

import (
	"github.com/bloxapp/ssv/network/forks"
	v0 "github.com/bloxapp/ssv/network/forks/v0"
	"github.com/bloxapp/ssv/utils/threadsafe"
	"go.uber.org/zap"
)

// ForkV1 is the genesis version 1 implementation
type ForkV1 struct {
	forkV0      forks.Fork
	forkSlot    uint64
	currentSlot *threadsafe.SafeUint64
	logger      *zap.Logger
}

// New returns an instance of ForkV0
func New(logger *zap.Logger, forkSlot uint64) forks.Fork {
	return &ForkV1{
		forkV0:      v0.New(),
		forkSlot:    forkSlot,
		currentSlot: threadsafe.Uint64(0),
		logger:      logger,
	}
}

// SlotTick implementation
func (v1 *ForkV1) SlotTick(slot uint64) {
	v1.currentSlot.Set(slot)

	if v1.forked() {
		v1.logger.Info("network forked to V1")
	}
}

// forked will return true if currentSlot >= forkSlot, meaning the fork happened
func (v1 *ForkV1) forked() bool {
	return v1.currentSlot.Get() >= v1.forkSlot
}
