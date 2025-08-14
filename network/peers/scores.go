package peers

import (
	"sync"

	"github.com/libp2p/go-libp2p/core/peer"
)

type PeerScore float64

// scoresIndex implements ScoreIndex
type scoresIndex struct {
	scores map[peer.ID][]*NodeScore
	lock   *sync.RWMutex
}

func newScoreIndex() ScoreIndex {
	return &scoresIndex{
		scores: map[peer.ID][]*NodeScore{},
		lock:   &sync.RWMutex{},
	}
}

// Score adds score to the given peer
func (s *scoresIndex) Score(id peer.ID, scores ...*NodeScore) error {
	s.lock.Lock()
	defer s.lock.Unlock()

scoresLoop:
	for _, score := range scores {
		existing, ok := s.scores[id]
		if !ok {
			existing = make([]*NodeScore, 0)
		}
		for _, current := range existing {
			if current.Name == score.Name {
				current.Value = score.Value
				continue scoresLoop
			}
		}
		s.scores[id] = append(existing, score)
	}
	return nil
}

// GetScore returns the desired score for the given peer
func (s *scoresIndex) GetScore(id peer.ID, names ...string) ([]NodeScore, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	peerScores, ok := s.scores[id]
	if !ok {
		return nil, nil
	}
	var scores []NodeScore
wantedScoresLoop:
	for _, name := range names {
		for _, score := range peerScores {
			if score.Name == name {
				scores = append(scores, *score)
				continue wantedScoresLoop
			}
		}
	}
	return scores, nil
}
