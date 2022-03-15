package v1

import (
	"github.com/bloxapp/ssv/network/forks"
)

// ForkV1 is the genesis version 0 implementation
type ForkV1 struct {
}

// New returns an instance of ForkV0
func New() forks.Fork {
	return &ForkV1{}
}
