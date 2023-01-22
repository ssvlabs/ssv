package queue

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv-spec/qbft"
)

// State represents a portion of the the current state
// that is relevant to the prioritization of messages.
type State struct {
	HasRunningInstance bool
	Height             qbft.Height
	Round              qbft.Round
	Slot               phase0.Slot
	Quorum             uint64
}

// MessagePrioritizer is an interface for prioritizing messages.
type MessagePrioritizer interface {
	// Prior returns true if message A should be prioritized over B.
	Prior(a, b *DecodedSSVMessage) bool
}

type standardPrioritizer struct {
	state *State
}

// NewMessagePrioritizer returns a standard implementation for MessagePrioritizer
// which prioritizes messages according to the given State.
func NewMessagePrioritizer(state *State) MessagePrioritizer {
	return &standardPrioritizer{state: state}
}

func (p *standardPrioritizer) Prior(a, b *DecodedSSVMessage) bool {
	msgScoreA, msgScoreB := messageScore(a), messageScore(b)
	if msgScoreA != msgScoreB {
		return msgScoreA > msgScoreB
	}

	relativeHeightA, relativeHeightB := compareHeightOrSlot(p.state, a), compareHeightOrSlot(p.state, b)
	if relativeHeightA != relativeHeightB {
		score := map[int]int{
			0:  2, // Current 1st.
			1:  1, // Higher 2nd.
			-1: 0, // Lower 3rd.
		}
		return score[relativeHeightA] > score[relativeHeightB]
	}

	scoreA, scoreB := messageTypeScore(p.state, a, relativeHeightA), messageTypeScore(p.state, b, relativeHeightB)
	if scoreA != scoreB {
		return scoreA > scoreB
	}

	scoreA, scoreB = compareRound(p.state, a), compareRound(p.state, b)
	if scoreA != scoreB {
		return scoreA > scoreB
	}

	scoreA, scoreB = consensusTypeScore(p.state, a), consensusTypeScore(p.state, b)
	if scoreA != scoreB {
		return scoreA > scoreB
	}

	return true
}
