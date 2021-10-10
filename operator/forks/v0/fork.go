package v0

import (
	ibftControllerFork "github.com/bloxapp/ssv/ibft/controller/forks"
	ibftControllerForkV0 "github.com/bloxapp/ssv/ibft/controller/forks/v0"
	networkForks "github.com/bloxapp/ssv/network/forks"
	storageForks "github.com/bloxapp/ssv/storage/forks"
)

type ForkV0 struct {
}

func New() *ForkV0 {
	return &ForkV0{}
}

func (v0 *ForkV0) IBFTControllerFork() ibftControllerFork.Fork {
	return ibftControllerForkV0.New()
}

func (v0 *ForkV0) NetworkFork() networkForks.Fork {
	return nil
}

func (v0 *ForkV0) StorageFork() storageForks.Fork {
	return nil
}
