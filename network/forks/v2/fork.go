package v2

import (
	"github.com/bloxapp/ssv/network/forks"
	"github.com/libp2p/go-libp2p"
	"time"
)

// ForkV2 is the genesis version 0 implementation
type ForkV2 struct {
}

// New returns an instance of ForkV0
func New() forks.Fork {
	return &ForkV2{}
}

// AddOptions implementation
func (v2 *ForkV2) AddOptions(opts []libp2p.Option) []libp2p.Option {
	opts = append(opts, libp2p.Ping(true))
	opts = append(opts, libp2p.EnableNATService())
	opts = append(opts, libp2p.AutoNATServiceRateLimit(15, 3, 1*time.Minute))
	//opts = append(opts, libp2p.DisableRelay())
	return opts
}
