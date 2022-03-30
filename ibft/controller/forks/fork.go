package forks

import (
	"github.com/bloxapp/ssv/ibft/instance/forks"
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/validator/storage"
)

// Fork holds all fork related implementations for the controller
type Fork interface {
	InstanceFork() forks.Fork
	ValidateDecidedMsg(share *storage.Share) pipeline.Pipeline
}
