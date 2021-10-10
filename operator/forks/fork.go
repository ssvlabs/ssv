package forks

import (
	ibftForks "github.com/bloxapp/ssv/ibft/forks"
	networkForks "github.com/bloxapp/ssv/network/forks"
	storageForks "github.com/bloxapp/ssv/storage/forks"
)

// Fork holds fork specific implementations for the various operator node component
type Fork interface {
	ibftForks.Fork
	networkForks.Fork
	storageForks.Fork
}
