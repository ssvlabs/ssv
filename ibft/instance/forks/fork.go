package forks

import (
	ibft2 "github.com/bloxapp/ssv/ibft"
)

// Fork will apply fork modifications on an ibft instance
type Fork interface {
	ibft2.Pipelines
	Apply(instance ibft2.Instance)
}
