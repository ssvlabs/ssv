package testing

import (
	"context"
	"fmt"

	"github.com/ssvlabs/ssv/network"
)

// NetworkFactory is a generic factory for network instances
type NetworkFactory func(pctx context.Context, nodeIndex uint64, keys NodeKeys) (network.P2PNetwork, error)

// NewLocalTestnet creates a new local network
func NewLocalTestnet(ctx context.Context, n int, factory NetworkFactory) ([]network.P2PNetwork, []NodeKeys, error) {
	nodes := make([]network.P2PNetwork, n)
	keys, err := CreateKeys(n)
	if err != nil {
		return nil, nil, err
	}

	for i, k := range keys {
		node, err := factory(ctx, uint64(i), k)
		if err != nil {
			// Close nodes already created so a factory failure (which makes
			// CreateAndStartLocalNet retry) doesn't leak them.
			for _, created := range nodes[:i] {
				if created != nil {
					_ = created.Close()
				}
			}
			return nil, nil, fmt.Errorf("create node %d: %w", i, err)
		}
		nodes[i] = node
	}

	return nodes, keys, nil
}
