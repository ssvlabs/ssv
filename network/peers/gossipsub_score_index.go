package peers

import (
	"sync"

	"github.com/bloxapp/ssv/network/topics/params"
	"github.com/libp2p/go-libp2p/core/peer"
)

// Implements GossipSubScoreIndex
type gossipSubScoreIndex struct {
	score map[peer.ID]float64
	mutex sync.Mutex

	graylistThreshold float64
}

func NewGossipSubScoreIndex() *gossipSubScoreIndex {

	graylistThreshold := params.PeerScoreThresholds().GraylistThreshold

	return &gossipSubScoreIndex{
		score:             make(map[peer.ID]float64),
		mutex:             sync.Mutex{},
		graylistThreshold: graylistThreshold,
	}
}

func (g *gossipSubScoreIndex) GetGossipSubScore(peerID peer.ID) (float64, bool) {
	g.mutex.Lock()
	defer g.mutex.Unlock()

	if score, exists := g.score[peerID]; exists {
		return score, true
	}
	return 0.0, false
}

func (g *gossipSubScoreIndex) SetScores(peerScores map[peer.ID]float64) {
	g.mutex.Lock()
	defer g.mutex.Unlock()

	g.clear()
	// Copy the map
	for peerID, score := range peerScores {
		g.score[peerID] = score
	}
}

func (g *gossipSubScoreIndex) clear() {
	g.score = make(map[peer.ID]float64)
}

func (g *gossipSubScoreIndex) HasBadGossipSubScore(peerID peer.ID) (bool, float64) {
	score, exists := g.GetGossipSubScore(peerID)
	if !exists {
		return false, 0.0
	}
	return (score <= g.graylistThreshold), score
}
