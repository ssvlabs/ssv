package connections

import (
	"fmt"
	"testing"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// DummyConnectionIndex is a mock implementation of ConnectionIndex for testing
type DummyConnectionIndex struct {
	ConnectedPeers map[peer.ID]network.Connectedness
	MaxPeers       int
	BadPeers       map[peer.ID]bool
}

func NewDummyConnectionIndex(maxPeers int) *DummyConnectionIndex {
	return &DummyConnectionIndex{
		ConnectedPeers: make(map[peer.ID]network.Connectedness),
		MaxPeers:       maxPeers,
		BadPeers:       make(map[peer.ID]bool),
	}
}

func (dci *DummyConnectionIndex) Limit() bool {
	return len(dci.ConnectedPeers) >= dci.MaxPeers
}

func (dci *DummyConnectionIndex) IsBad(id peer.ID) bool {
	if bad, ok := dci.BadPeers[id]; ok {
		return bad
	}
	return false
}

////////////////////////////////////////////////////////////////////////////

func setupTestEnvironment(maxPeers int) (*ConnectionGater, *DummyConnectionIndex, peer.ID) {
	logger := zap.NewExample()
	testPeerID, _ := peer.Decode("12D3KooWEd...") // Example peer ID
	dummyIndex := NewDummyConnectionIndex(maxPeers)
	gater := NewConnectionGater(logger, dummyIndex.Limit, dummyIndex.IsBad)

	return gater, dummyIndex, testPeerID
}

func TestBlockPeer(t *testing.T) {
	gater, _, testPeerID := setupTestEnvironment(5)

	gater.BlockPeer(testPeerID)
	assert.True(t, gater.IsPeerBlocked(testPeerID), "Peer %v was not blocked as expected", testPeerID)
}

func TestIsPeerBlocked(t *testing.T) {
	gater, _, testPeerID := setupTestEnvironment(5)

	assert.False(t, gater.IsPeerBlocked(testPeerID), "Peer %v is unexpectedly blocked", testPeerID)
}

func TestInterceptPeerDial(t *testing.T) {
	gater, dummyIndex, testPeerID := setupTestEnvironment(5)

	// Simulate that the number of connected peers is below the limit
	for i := 0; i < 4; i++ {
		dummyPeerID := peer.ID(fmt.Sprintf("Peer%d", i))
		dummyIndex.ConnectedPeers[dummyPeerID] = network.Connected
	}

	// Test peer dial when under the limit
	assert.True(t, gater.InterceptPeerDial(testPeerID), "InterceptPeerDial should allow dialing when under peer limit")

	// Add another peer to reach the limit
	dummyIndex.ConnectedPeers[peer.ID("Peer4")] = network.Connected

	// Test peer dial when at the limit
	assert.False(t, gater.InterceptPeerDial(testPeerID), "InterceptPeerDial should block dialing when at peer limit")
}

func TestInterceptSecured(t *testing.T) {
	gater, dummyIndex, testPeerID := setupTestEnvironment(5)

	// Simulate a good peer
	dummyIndex.BadPeers[testPeerID] = false

	// Test connection with a good peer
	assert.True(t, gater.InterceptSecured(network.DirOutbound, testPeerID, nil), "InterceptSecured should allow connection with a good peer")

	// Mark the peer as bad
	dummyIndex.BadPeers[testPeerID] = true

	// Test connection with a bad peer
	assert.False(t, gater.InterceptSecured(network.DirOutbound, testPeerID, nil), "InterceptSecured should block connection with a bad peer")
}
