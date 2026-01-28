package qbft

import (
	"sort"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// Deprecated - only for pre-Boole fork
func RoundRobinProposerPreBooleFork(state *specqbft.State, round specqbft.Round) spectypes.OperatorID {
	firstRoundIndex := uint64(0)
	if state.Height != specqbft.FirstHeight {
		firstRoundIndex += uint64(state.Height) % uint64(len(state.CommitteeMember.Committee))
	}

	index := (firstRoundIndex + uint64(round) - uint64(specqbft.FirstRound)) % uint64(len(state.CommitteeMember.Committee))
	return state.CommitteeMember.Committee[index].OperatorID
}

type networkConfig interface {
	EstimatedEpochAtSlot(slot phase0.Slot) phase0.Epoch
}

// RoundRobinProposer returns the proposer for the round.
// Each new height starts with the first proposer and increments by 1 with each following round.
// Each new height has a different first round proposer which is +1 from the previous height.
// Also, the current Ethereum epoch is taken into account to introduce variability through epochs
// (mostly for committees with 4 operators, as 32%4 = 0 as the epochs would "repeat" otherwise).
func RoundRobinProposer(
	height specqbft.Height,
	round specqbft.Round,
	committee []spectypes.OperatorID,
	netCfg networkConfig,
) spectypes.OperatorID {
	// Ensure committee is sorted to avoid proposer depending on input order.
	if !sort.SliceIsSorted(committee, func(i, j int) bool { return committee[i] < committee[j] }) {
		sorted := make([]spectypes.OperatorID, len(committee))
		copy(sorted, committee)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
		committee = sorted
	}

	ethEpoch := uint64(netCfg.EstimatedEpochAtSlot(phase0.Slot(height)))

	firstRoundIndex := uint64(0)
	if height != specqbft.FirstHeight {
		firstRoundIndex += uint64(height) % uint64(len(committee))
	}

	index := (firstRoundIndex + uint64(round) - uint64(specqbft.FirstRound) + ethEpoch) % uint64(len(committee))
	return committee[index]
}
