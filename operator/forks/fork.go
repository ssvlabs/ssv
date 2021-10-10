package forks

import (
	ibftControllerFork "github.com/bloxapp/ssv/ibft/controller/forks"
	networkForks "github.com/bloxapp/ssv/network/forks"
	storageForks "github.com/bloxapp/ssv/storage/forks"
)

// Fork holds fork specific implementations for the various operator node component
type Fork interface {
	IBFTControllerFork() ibftControllerFork.Fork
	NetworkFork() networkForks.Fork
	StorageFork() storageForks.Fork
}
