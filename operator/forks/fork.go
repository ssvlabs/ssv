package forks

import (
	ibftControllerFork "github.com/bloxapp/ssv/ibft/controller/forks"
	networkForks "github.com/bloxapp/ssv/network/forks"
	storageForks "github.com/bloxapp/ssv/storage/forks"
)

const (
	// NetworkV1ForkSlot - slot for the fork
	NetworkV1ForkSlot = 1713026 // Approx. 16 Nov 2021 at Noon UTC
)

// Fork holds fork specific implementations for the various operator node component
type Fork interface {
	SlotTick(slot uint64)
	IBFTControllerFork() ibftControllerFork.Fork
	NetworkFork() networkForks.Fork
	StorageFork() storageForks.Fork
}
