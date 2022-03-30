package forksfactory

import (
	"github.com/bloxapp/ssv/ibft/controller/forks"
	forksv0 "github.com/bloxapp/ssv/ibft/controller/forks/v0"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
)

// NewFork returns a new fork instance from the given version
func NewFork(forkVersion forksprotocol.ForkVersion) forks.Fork {
	switch forkVersion {
	case forksprotocol.V0ForkVersion:
		return &forksv0.ForkV0{}
	case forksprotocol.V1ForkVersion:
		// TODO:
		return &forksv0.ForkV0{}
	default:
		return nil
	}
}
