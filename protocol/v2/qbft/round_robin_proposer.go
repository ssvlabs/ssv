package qbft

import (
	"github.com/ssvlabs/ssv-spec/qbft"
	"github.com/ssvlabs/ssv-spec/types"
)

// RoundRobinProposer returns the proposer for the round.
// Each new height starts with the first proposer and increments by 1 with each following round.
// Each new height has a different first round proposer which is +1 from the previous height.
// First height starts with index 0
func RoundRobinProposer(committee []*types.Operator, height qbft.Height, round qbft.Round) types.OperatorID {
	firstRoundIndex := uint64(0)

	if height != qbft.FirstHeight {
		firstRoundIndex += uint64(height) % uint64(len(committee))
	}

	index := (firstRoundIndex + uint64(round) - uint64(qbft.FirstRound)) % uint64(len(committee))

	return committee[index].OperatorID
}
