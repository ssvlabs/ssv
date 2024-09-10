package peers

import (
	"sync"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/ssvlabs/ssv/network/topics/params"
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

func (g *gossipSubScoreIndex) AddScore(peerID peer.ID, score float64) {
	g.mutex.Lock()
	defer g.mutex.Unlock()

	g.score[peerID] = score
}

func (g *gossipSubScoreIndex) Clear() {
	g.mutex.Lock()
	defer g.mutex.Unlock()

	g.score = make(map[peer.ID]float64)
}

func (g *gossipSubScoreIndex) HasBadGossipSubScore(peerID peer.ID) (bool, float64) {
	score, exists := g.GetGossipSubScore(peerID)
	if !exists {
		return false, 0.0
	}
	return (score <= g.graylistThreshold), score
}
