package mock

import (
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"
)

// MockConnectionIndex is a mock implementation of the ConnectionIndex interface
type MockConnectionIndex struct {
	LimitValue bool
}

// Connectedness panics if called
func (m *MockConnectionIndex) Connectedness(id peer.ID) network.Connectedness {
	panic("Connectedness method is not implemented in MockConnectionIndex")
}

// CanConnect panics if called
func (m *MockConnectionIndex) CanConnect(id peer.ID) bool {
	panic("CanConnect method is not implemented in MockConnectionIndex")
}

// Limit returns the mock value for Limit
func (m *MockConnectionIndex) Limit(dir network.Direction) bool {
	return m.LimitValue
}

// IsBad panics if called
func (m *MockConnectionIndex) IsBad(logger *zap.Logger, id peer.ID) bool {
	panic("IsBad method is not implemented in MockConnectionIndex")
}
