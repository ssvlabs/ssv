package forks

import (
	"github.com/bloxapp/ssv/ibft"
)

// Fork will apply fork modifications on an ibft instance
type Fork interface {
	ibft.Pipelines
	Apply(instance ibft.Instance)
}
