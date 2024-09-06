package peers

import (
	"sync"

	"github.com/libp2p/go-libp2p/core/peer"
)

// BadPeersCollector serves as an interface to register
// bad peers that should be disconnected.
type BadPeersCollector interface {
	// Registers a peer considered to be bad, along with its score
	RegisterBadPeer(peerID peer.ID, score float64)
	// Returns whether a peer is bad or not. If so, also returns the peer's score
	IsBad(peerID peer.ID) (bool, float64)
	// Clears the collector
	Clear()
}

// Implements BadPeersCollector
type badPeersCollector struct {
	badPeers map[peer.ID]float64 // Peers that should be disconnected from
	mutex    sync.Mutex
}

func NewBadPeersCollector() *badPeersCollector {
	return &badPeersCollector{
		badPeers: make(map[peer.ID]float64),
		mutex:    sync.Mutex{},
	}
}

func (c *badPeersCollector) RegisterBadPeer(peerID peer.ID, score float64) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.badPeers[peerID] = score
}

func (c *badPeersCollector) IsBad(peerID peer.ID) (bool, float64) {
	if score, exists := c.badPeers[peerID]; exists {
		return true, score
	}
	return false, 0.0
}

func (c *badPeersCollector) Clear() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.badPeers = make(map[peer.ID]float64)
}
