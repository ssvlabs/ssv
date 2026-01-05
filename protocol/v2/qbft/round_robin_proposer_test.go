package qbft

import (
	"testing"

	"github.com/stretchr/testify/require"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
)

func TestRoundRobinIndex_CommitteeSizeZero(t *testing.T) {
	require.Equal(t, uint64(0), RoundRobinIndex(1, specqbft.FirstRound, 0, 0))
}

func TestRoundRobinIndex_OffsetAffectsResult(t *testing.T) {
	height := specqbft.Height(20)
	round := specqbft.FirstRound
	committeeSize := uint64(4)

	gotNoOffset := RoundRobinIndex(height, round, committeeSize, 0)
	gotOffset := RoundRobinIndex(height, round, committeeSize, 2)

	require.Equal(t, (gotNoOffset+2)%committeeSize, gotOffset)
}
