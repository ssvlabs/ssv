package testing

import (
	"context"

	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/ssvlabs/ssv/network"
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

// NewLocalTestnet creates a new local network from test keys set
func NewLocalTestnetFromKeySet(ctx context.Context, factory NetworkFactory, ks *spectestingutils.TestKeySet) ([]network.P2PNetwork, []NodeKeys, error) {
	nodes := make([]network.P2PNetwork, len(ks.OperatorKeys))
	keys, err := CreateKeysFromKeySet(ks)
	if err != nil {
		return nil, nil, err
	}

	for i, k := range keys {
		nodes[i] = factory(ctx, i, k)
	}

	return nodes, keys, nil
}
