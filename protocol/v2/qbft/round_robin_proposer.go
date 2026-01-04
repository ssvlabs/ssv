package qbft

import (
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

func ForkAwareRoundRobinProposer(state *specqbft.State, round specqbft.Round, booleFork bool) spectypes.OperatorID {
	if booleFork {
		return RoundRobinProposer_Boole(state, round)
	}
	return RoundRobinProposer(state, round)
}

// RoundRobinProposer returns the proposer for the round.
// Each new height starts with the first proposer and increments by 1 with each following round.
// Each new height has a different first round proposer which is +1 from the previous height.
// First height starts with index 0
func RoundRobinProposer(state *specqbft.State, round specqbft.Round) spectypes.OperatorID {
	firstRoundIndex := uint64(0)
	if state.Height != specqbft.FirstHeight {
		firstRoundIndex += uint64(state.Height) % uint64(len(state.CommitteeMember.Committee))
	}

	index := (firstRoundIndex + uint64(round) - uint64(specqbft.FirstRound)) % uint64(len(state.CommitteeMember.Committee))
	return state.CommitteeMember.Committee[index].OperatorID
}

// RoundRobinProposer_Boole returns the proposer for the round.
// Each new height starts with the first proposer and increments by 1 with each following round.
// Each new height has a different first round proposer which is +1 from the previous height.
// Also, the current Ethereum epoch is taken into account to introduce variability through epochs
// (mostly for committees with 4 operators, as 32%4 = 0 as the epochs would "repeat" otherwise).
// First height starts with index 0
func RoundRobinProposer_Boole(state *specqbft.State, round specqbft.Round) spectypes.OperatorID {
	firstRoundIndex := uint64(0)
	if state.Height != specqbft.FirstHeight {
		firstRoundIndex += uint64(state.Height) % uint64(len(state.CommitteeMember.Committee))
	}
	ethEpoch := uint64(state.Height) / 32

	index := (firstRoundIndex + uint64(round) - uint64(specqbft.FirstRound) + ethEpoch) % uint64(len(state.CommitteeMember.Committee))
	return state.CommitteeMember.Committee[index].OperatorID
}
