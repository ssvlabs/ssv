package forks

import (
	"github.com/bloxapp/ssv/ibft/instance/forks"
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/protocol/v1/keymanager"
)

// Fork holds all fork related implementations for the controller
type Fork interface {
	InstanceFork() forks.Fork
	ValidateDecidedMsg(share *keymanager.Share) pipeline.Pipeline
}
