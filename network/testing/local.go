//go:build testutils

// This file contains helpers for tests only.
// It will not be compiled into production binaries.

package testing

import (
	"context"

	"github.com/ssvlabs/ssv/network"
)

// NetworkFactory is a generic factory for network instances
type NetworkFactory func(pctx context.Context, nodeIndex uint64, keys NodeKeys) network.P2PNetwork

// NewLocalTestnet creates a new local network
func NewLocalTestnet(ctx context.Context, n int, factory NetworkFactory) ([]network.P2PNetwork, []NodeKeys, error) {
	nodes := make([]network.P2PNetwork, n)
	keys, err := CreateKeys(n)
	if err != nil {
		return nil, nil, err
	}

	i := uint64(0)
	for _, k := range keys {
		nodes[i] = factory(ctx, i, k)
		i++
	}

	return nodes, keys, nil
}
