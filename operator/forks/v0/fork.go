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
}

// New returns a new ForkV0 instance
func New() *ForkV0 {
	return &ForkV0{}
}

// IBFTControllerFork returns ibft controller fork
func (v0 *ForkV0) IBFTControllerFork() ibftControllerFork.Fork {
	return ibftControllerForkV0.New()
}

// NetworkFork returns network fork
func (v0 *ForkV0) NetworkFork() networkForks.Fork {
	return networkForkV0.New()
}

// StorageFork returns storage fork
func (v0 *ForkV0) StorageFork() storageForks.Fork {
	return nil
}
