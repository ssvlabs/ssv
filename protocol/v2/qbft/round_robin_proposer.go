package qbft

import (
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
)

// RoundRobinIndex returns the committee index of the proposer for a given height/round.
// offset can be used by callers to introduce additional deterministic variability (e.g. based on epoch).
// For example: Boole Fork sets the offset to the current epoch to ensure fair proposer rotation across epochs.
// see: https://github.com/ssvlabs/ssv-spec/pull/591
func RoundRobinIndex(height specqbft.Height, round specqbft.Round, committeeSize uint64, offset uint64) uint64 {
	if committeeSize == 0 {
		return 0
	}

	firstRoundIndex := uint64(0)
	if height != specqbft.FirstHeight {
		firstRoundIndex += uint64(height) % committeeSize
	}

	return (firstRoundIndex + uint64(round) - uint64(specqbft.FirstRound) + offset) % committeeSize
}
