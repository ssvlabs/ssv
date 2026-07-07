package qbft

import (
	"slices"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// roundRobinProposerPreBooleFork is the pre-Boole proposer selection. Kept only for
// heights before the Boole fork; new code should go through Proposer.
func roundRobinProposerPreBooleFork(height specqbft.Height, round specqbft.Round, committee []spectypes.OperatorID) spectypes.OperatorID {
	firstRoundIndex := uint64(0)
	if height != specqbft.FirstHeight {
		firstRoundIndex += uint64(height) % uint64(len(committee))
	}

	index := (firstRoundIndex + uint64(round) - uint64(specqbft.FirstRound)) % uint64(len(committee))
	return committee[index]
}

type networkConfig interface {
	EstimatedEpochAtSlot(slot phase0.Slot) phase0.Epoch
	BooleForkAtSlot(slot phase0.Slot) bool
}

// RoundRobinProposer returns the proposer for the round.
// Each new height starts with the first proposer and increments by 1 with each following round.
// Each new height has a different first round proposer which is +1 from the previous height
// (+2 when the height crosses an epoch boundary, as the epoch offset advances as well).
// Also, the current Ethereum epoch is taken into account to introduce variability through epochs
// (mostly for committees with 4 operators, as 32%4 = 0 as the epochs would "repeat" otherwise).
func RoundRobinProposer(
	height specqbft.Height,
	round specqbft.Round,
	committee []spectypes.OperatorID,
	netCfg networkConfig,
) spectypes.OperatorID {
	// Ensure committee is sorted to avoid proposer depending on input order.
	if !slices.IsSorted(committee) {
		committee = slices.Clone(committee)
		slices.Sort(committee)
	}

	ethEpoch := uint64(netCfg.EstimatedEpochAtSlot(phase0.Slot(height)))

	firstRoundIndex := uint64(0)
	if height != specqbft.FirstHeight {
		firstRoundIndex += uint64(height) % uint64(len(committee))
	}

	index := (firstRoundIndex + uint64(round) - uint64(specqbft.FirstRound) + ethEpoch) % uint64(len(committee))
	return committee[index]
}

// Proposer returns the round-robin proposer for the given height/round, selecting the
// fork-appropriate algorithm from netCfg. Both the runners (via ProposerF) and message
// validation call this, so the two always agree on the leader per fork.
func Proposer(height specqbft.Height, round specqbft.Round, committee []spectypes.OperatorID, netCfg networkConfig) spectypes.OperatorID {
	if netCfg.BooleForkAtSlot(phase0.Slot(height)) {
		return RoundRobinProposer(height, round, committee, netCfg)
	}
	return roundRobinProposerPreBooleFork(height, round, committee)
}
