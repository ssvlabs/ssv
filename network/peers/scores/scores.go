package scores

import (
	"sort"
	"sync"

	"github.com/libp2p/go-libp2p/core/peer"
)

type PeerScore float64

// NodeScore is a wrapping object for scores
type NodeScore struct {
	Name  string
	Value float64
}

// ScoreIndex is an interface for managing peers scores
type ScoreIndex interface {
	// Score adds score to the given peer
	Score(id peer.ID, scores ...*NodeScore) error
	// GetScore returns the desired score for the given peer
	GetScore(id peer.ID, names ...string) ([]NodeScore, error)
}

// scoresIndex implements ScoreIndex
type scoresIndex struct {
	scores map[peer.ID][]*NodeScore
	lock   *sync.RWMutex
}

func NewScoreIndex() ScoreIndex {
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

// GetTopScores accepts a map of scores and returns the best n peers
func GetTopScores(peerScores map[peer.ID]PeerScore, n int) map[peer.ID]PeerScore {
	pl := make(peerScoresList, len(peerScores))
	i := 0
	for k, v := range peerScores {
		pl[i] = peerScorePair{k, v}
		i++
	}
	sort.Sort(sort.Reverse(pl))
	res := make(map[peer.ID]PeerScore)
	for _, item := range pl {
		res[item.Key] = item.Score
		if len(res) >= n {
			break
		}
	}
	return res
}

type peerScorePair struct {
	Key   peer.ID
	Score PeerScore
}

type peerScoresList []peerScorePair

func (p peerScoresList) Len() int           { return len(p) }
func (p peerScoresList) Less(i, j int) bool { return p[i].Score < p[j].Score }
func (p peerScoresList) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }
