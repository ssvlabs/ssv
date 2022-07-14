package forksfactory

import (
	"github.com/bloxapp/ssv/ibft/storage/forks"
	"github.com/bloxapp/ssv/ibft/storage/forks/genesis"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
)

// NewFork returns a new fork instance from the given version
func NewFork(forkVersion forksprotocol.ForkVersion) forks.Fork {
	switch forkVersion {
	case forksprotocol.GenesisForkVersion:
		return &genesis.ForkGenesis{}
	default:
		return nil
	}
}
