package forks

import (
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation"
)

// Fork will apply fork modifications on an ibft instance
type Fork interface {
	validation.Pipelines
	Apply(instance *instance.Instance)
}
