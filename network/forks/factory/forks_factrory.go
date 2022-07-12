package factory

import (
	"github.com/bloxapp/ssv/network/forks"
	"github.com/bloxapp/ssv/network/forks/genesis"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
)

// NewFork returns a new fork instance from the given version
func NewFork(forkVersion forksprotocol.ForkVersion) forks.Fork {
	switch forkVersion {
	case forksprotocol.GenesisForkVersion:
		return &genesis.ForkGenesis{}
	default:
		return &genesis.ForkGenesis{}
	}
}
