package forks

import (
	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/ibft/instance/forks"
	"github.com/bloxapp/ssv/ibft/pipeline"
)

// Fork holds all fork related implementations for the controller
type Fork interface {
	Apply(controller ibft.Controller)
	InstanceFork() forks.Fork
	ValidateDecidedMsg() pipeline.Pipeline
}
