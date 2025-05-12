package queue

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ssvlabs/ssv-spec/qbft"
)

// State represents a portion of the current state
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
	Prior(a, b *SSVMessage) bool
}

type standardPrioritizer struct {
	state *State
}

// NewMessagePrioritizer returns a standard implementation for MessagePrioritizer
// which prioritizes messages according to the given State.
func NewMessagePrioritizer(state *State) MessagePrioritizer {
	return &standardPrioritizer{state: state}
}

func (p *standardPrioritizer) Prior(a, b *SSVMessage) bool {
	msgScoreA, msgScoreB := scoreMessageType(a), scoreMessageType(b)
	if msgScoreA != msgScoreB {
		return msgScoreA > msgScoreB
	}

	relativeHeightA, relativeHeightB := compareHeightOrSlot(p.state, a), compareHeightOrSlot(p.state, b)
	if relativeHeightA != relativeHeightB {
		return scoreHeight(relativeHeightA) > scoreHeight(relativeHeightB)
	}

	scoreA, scoreB := scoreMessageSubtype(p.state, a, relativeHeightA), scoreMessageSubtype(p.state, b, relativeHeightB)
	if scoreA != scoreB {
		return scoreA > scoreB
	}

	scoreA, scoreB = scoreRound(p.state, a), scoreRound(p.state, b)
	if scoreA != scoreB {
		return scoreA > scoreB
	}

	scoreA, scoreB = scoreConsensusType(a), scoreConsensusType(b)
	if scoreA != scoreB {
		return scoreA > scoreB
	}

	return true
}

func scoreHeight(relativeHeight int) int {
	switch relativeHeight {
	case 0:
		return 2
	case 1:
		return 1
	case -1:
		return 0
	}
	return 0
}

func NewCommitteeQueuePrioritizer(state *State) MessagePrioritizer {
	return &committeePrioritizer{state: state}
}

type committeePrioritizer struct {
	state *State
}

func (p *committeePrioritizer) Prior(a, b *SSVMessage) bool {
	msgScoreA, msgScoreB := scoreMessageType(a), scoreMessageType(b)
	if msgScoreA != msgScoreB {
		return msgScoreA > msgScoreB
	}

	relativeHeightA, relativeHeightB := compareHeightOrSlot(p.state, a), compareHeightOrSlot(p.state, b)
	if relativeHeightA != relativeHeightB {
		return scoreHeight(relativeHeightA) > scoreHeight(relativeHeightB)
	}

	scoreA, scoreB := scoreCommitteeMessageSubtype(p.state, a, relativeHeightA), scoreCommitteeMessageSubtype(p.state, b, relativeHeightB)
	if scoreA != scoreB {
		return scoreA > scoreB
	}

	scoreA, scoreB = scoreConsensusType(a), scoreConsensusType(b)
	if scoreA != scoreB {
		return scoreA > scoreB
	}

	return true
}
