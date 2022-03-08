package testing

import (
	"context"
	"github.com/bloxapp/ssv/network"
)

// NetworkFactory is a generic factory for network instances
type NetworkFactory func(pctx context.Context, keys NodeKeys) network.V1

// NewLocalNetwork creates a new local network
func NewLocalNetwork(ctx context.Context, n int, factory NetworkFactory) ([]network.V1, []NodeKeys, error) {
	nodes := make([]network.V1, n)
	keys, err := CreateKeys(n)
	if err != nil {
		return nil, nil, err
	}

	for i, k := range keys {
		nodes[i] = factory(ctx, k)
	}

	return nodes, keys, nil
}
