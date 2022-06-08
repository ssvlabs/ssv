package v0

import (
	"github.com/bloxapp/ssv/network/forks"
	"github.com/libp2p/go-libp2p"
)

// ForkV0 is the genesis version 0 implementation
type ForkV0 struct {
}

// New returns an instance of ForkV0
func New() forks.Fork {
	return &ForkV0{}
}

// AddOptions implementation
func (v0 *ForkV0) AddOptions(opts []libp2p.Option) []libp2p.Option {
	opts = append(opts, libp2p.Ping(false))
	opts = append(opts, libp2p.DisableRelay())
	//opts = append(opts, libp2p.EnableNATService())
	//opts = append(opts, libp2p.AutoNATServiceRateLimit(15, 3, 1*time.Minute))
	return opts
}
