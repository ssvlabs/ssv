package qbft

import (
	"github.com/ssvlabs/ssv-spec/qbft"
	"github.com/ssvlabs/ssv-spec/types"
)

// RoundRobinProposer returns the proposer for the round.
// Each new height starts with the first proposer and increments by 1 with each following round.
// Each new height has a different first round proposer which is +1 from the previous height.
// First height starts with index 0
func RoundRobinProposer(state *qbft.State, round qbft.Round) types.OperatorID {
	firstRoundIndex := uint64(0)
	if state.Height != qbft.FirstHeight {
		firstRoundIndex += uint64(state.Height) % uint64(len(state.CommitteeMember.Committee))
	}

	index := (firstRoundIndex + uint64(round) - uint64(qbft.FirstRound)) % uint64(len(state.CommitteeMember.Committee))
	return state.CommitteeMember.Committee[index].OperatorID
}
