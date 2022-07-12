package forksfactory

import (
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/protocol/v1/qbft/controller/forks"
	"github.com/bloxapp/ssv/protocol/v1/qbft/controller/forks/genesis"
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
