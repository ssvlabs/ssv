package testing

import (
	"context"

	"github.com/bloxapp/ssv/network"
)

// NetworkFactory is a generic factory for network instances
type NetworkFactory func(pctx context.Context, nodeIndex int, keys NodeKeys) network.P2PNetwork

// NewLocalTestnet creates a new local network
func NewLocalTestnet(ctx context.Context, n int, factory NetworkFactory) ([]network.P2PNetwork, []NodeKeys, error) {
	nodes := make([]network.P2PNetwork, n)
	keys, err := CreateKeys(n)
	if err != nil {
		return nil, nil, err
	}

	for i, k := range keys {
		nodes[i] = factory(ctx, i, k)
	}

	return nodes, keys, nil
}
