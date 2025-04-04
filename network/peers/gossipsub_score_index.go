package peers

import (
	"sync"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/ssvlabs/ssv/network/topics/params"
)

// Implements GossipScoreIndex
type gossipScoreIndex struct {
	score map[peer.ID]float64
	mutex sync.RWMutex

	graylistThreshold float64
}

func NewGossipScoreIndex() *gossipScoreIndex {

	graylistThreshold := params.PeerScoreThresholds().GraylistThreshold

	return &gossipScoreIndex{
		score:             make(map[peer.ID]float64),
		graylistThreshold: graylistThreshold,
	}
}

func (g *gossipScoreIndex) GetGossipScore(peerID peer.ID) (float64, bool) {
	g.mutex.RLock()
	defer g.mutex.RUnlock()

	if score, exists := g.score[peerID]; exists {
		return score, true
	}
	return 0.0, false
}

func (g *gossipScoreIndex) SetScores(peerScores map[peer.ID]float64) {
	g.mutex.Lock()
	defer g.mutex.Unlock()

	g.clear()
	// Copy the map
	for peerID, score := range peerScores {
		g.score[peerID] = score
	}
}

func (g *gossipScoreIndex) clear() {
	g.score = make(map[peer.ID]float64)
}

func (g *gossipScoreIndex) HasBadGossipScore(peerID peer.ID) (bool, float64) {
	score, exists := g.GetGossipScore(peerID)
	if !exists {
		return false, 0.0
	}
	return (score <= g.graylistThreshold), score
}
