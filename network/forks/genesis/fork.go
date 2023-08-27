package genesis

import (
	"time"

	"github.com/bloxapp/ssv/network/forks"
	"github.com/libp2p/go-libp2p"
)

// ForkGenesis is the genesis version 0 implementation
type ForkGenesis struct {
	topics []string
}

// New returns an instance of ForkV0
func New() forks.Fork {
	g := &ForkGenesis{}
	g.topics = make([]string, g.Subnets())
	for i := 0; i < g.Subnets(); i++ {
		g.topics[i] = g.GetTopicFullName(g.SubnetTopicID(i))
	}
	return g
}

// AddOptions implementation
func (g *ForkGenesis) AddOptions(opts []libp2p.Option) []libp2p.Option {
	opts = append(opts, libp2p.Ping(true))
	opts = append(opts, libp2p.EnableNATService())
	opts = append(opts, libp2p.AutoNATServiceRateLimit(15, 3, 1*time.Minute))
	// opts = append(opts, libp2p.DisableRelay())
	return opts
}
