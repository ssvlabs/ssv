package simulator

import (
	"github.com/ethereum/go-ethereum/node"
)

// Node is ONLY change in this file is exposing the *node.Node private field to get the RPCHandler for tests
func (b *Backend) Node() *node.Node {
	return b.node
}
