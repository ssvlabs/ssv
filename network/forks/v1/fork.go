package v1

import (
	"github.com/bloxapp/ssv/network/forks"
	"github.com/libp2p/go-libp2p"
	"time"
)

// ForkV1 is the genesis version 0 implementation
type ForkV1 struct {
}

// New returns an instance of ForkV0
func New() forks.Fork {
	return &ForkV1{}
}

// AddOptions implementation
func (v1 *ForkV1) AddOptions(opts []libp2p.Option) []libp2p.Option {
	opts = append(opts, libp2p.Ping(true))
	opts = append(opts, libp2p.EnableNATService())
	opts = append(opts, libp2p.AutoNATServiceRateLimit(15, 3, 1*time.Minute))
	//opts = append(opts, libp2p.DisableRelay())
	return opts
}
