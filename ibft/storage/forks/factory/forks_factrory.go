package forksfactory

import (
	"github.com/bloxapp/ssv/ibft/storage/forks"
	v0 "github.com/bloxapp/ssv/ibft/storage/forks/v0"
	v1 "github.com/bloxapp/ssv/ibft/storage/forks/v1"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
)

// NewFork returns a new fork instance from the given version
func NewFork(forkVersion forksprotocol.ForkVersion) forks.Fork {
	switch forkVersion {
	case forksprotocol.V0ForkVersion:
		return &v0.ForkV0{}
	case forksprotocol.V1ForkVersion, forksprotocol.V2ForkVersion: // v2 has no different from v1
		return &v1.ForkV1{}
	default:
		return nil
	}
}
