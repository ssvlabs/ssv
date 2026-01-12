package qbft

import (
	"sort"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

type networkConfig interface {
	EstimatedEpochAtSlot(slot phase0.Slot) phase0.Epoch
	BooleForkAtEpoch(epoch phase0.Epoch) bool
}

// RoundRobinProposer returns the proposer for the round.
// Each new height starts with the first proposer and increments by 1 with each following round.
// Each new height has a different first round proposer which is +1 from the previous height.
// First height starts with index 0.
// Boole fork adds an epoch-derived offset (from network config) to introduce additional variability.
func RoundRobinProposer(
	height specqbft.Height,
	round specqbft.Round,
	committee []spectypes.OperatorID,
	netCfg networkConfig) spectypes.OperatorID {
	if !sort.SliceIsSorted(committee, func(i, j int) bool { return committee[i] < committee[j] }) {
		sorted := make([]spectypes.OperatorID, len(committee))
		copy(sorted, committee)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
		committee = sorted
	}

	// compute epoch-derived offset for variability (= 0 in pre-Boole fork), name ethEpoch kept from spec code.
	ethEpoch := uint64(0)
	epoch := netCfg.EstimatedEpochAtSlot(phase0.Slot(height))
	if netCfg.BooleForkAtEpoch(epoch) {
		ethEpoch = uint64(epoch)
	}

	firstRoundIndex := uint64(0)
	if height != specqbft.FirstHeight {
		firstRoundIndex += uint64(height) % uint64(len(committee))
	}

	index := (firstRoundIndex + uint64(round) - uint64(specqbft.FirstRound) + ethEpoch) % uint64(len(committee))
	return committee[index]
}
