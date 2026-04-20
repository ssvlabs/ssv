package mock

import (
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

// MockConnectionIndex is a mock implementation of the ConnectionIndex interface
type MockConnectionIndex struct {
	LimitValue         bool
	ConnectednessValue network.Connectedness
	BadPeers           map[peer.ID]bool
}

func (m *MockConnectionIndex) Connectedness(id peer.ID) network.Connectedness {
	return m.ConnectednessValue
}

// CanConnect panics if called
func (m *MockConnectionIndex) CanConnect(id peer.ID) error {
	panic("CanConnect method is not implemented in MockConnectionIndex")
}

// AtLimit returns the mock value for Limit
func (m *MockConnectionIndex) AtLimit(dir network.Direction) bool {
	return m.LimitValue
}

func (m *MockConnectionIndex) IsBad(id peer.ID) bool {
	return m.BadPeers[id]
}
