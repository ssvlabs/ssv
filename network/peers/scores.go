package peers

import (
	"fmt"
	"sort"
	"sync"

	"github.com/libp2p/go-libp2p/core/peer"
)

// scoresIndex implements ScoreIndex
type scoresIndex struct {
	scores    map[peer.ID]float64
	threshold float64
	lock      *sync.RWMutex
}

func newScoreIndex() ScoreIndex {
	return &scoresIndex{
		scores: map[peer.ID]float64{},
		lock:   &sync.RWMutex{},
	}
}

// Score adds score to the given peer
func (s *scoresIndex) SetThreshold(threshold float64) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.threshold = threshold
}

// Score adds score to the given peer
func (s *scoresIndex) Score(id peer.ID, score float64) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.scores[id] = score
}

// GetScore returns the desired score for the given peer
func (s *scoresIndex) GetScore(id peer.ID) (float64, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	peerScores, ok := s.scores[id]
	if !ok {
		return 0, fmt.Errorf("peer has not score yet")
	}

	return peerScores, nil
}

// Score adds score to the given peer
func (s *scoresIndex) IsBelowThreshold(id peer.ID) bool {
	s.lock.RLock()
	defer s.lock.RUnlock()

	peerScores, ok := s.scores[id]
	if !ok {
		return false
	}

	return peerScores < s.threshold
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
