//go:build alan_spec

package qbft

import (
	"testing"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectests "github.com/ssvlabs/ssv-spec/qbft/spectest/tests"
	"github.com/stretchr/testify/require"

	qbftimpl "github.com/ssvlabs/ssv/protocol/v2/qbft"
)

func runRoundRobinSpecTest(t *testing.T, test *spectests.RoundRobinSpecTest) {
	require.Equal(t, len(test.Heights), len(test.Rounds))
	require.Equal(t, len(test.Heights), len(test.Proposers))

	for i, height := range test.Heights {
		state := &specqbft.State{
			Height:          height,
			Round:           test.Rounds[i],
			CommitteeMember: test.Share,
		}
		got := qbftimpl.RoundRobinProposerPreBooleFork(state, test.Rounds[i])
		require.EqualValues(t, test.Proposers[i], got)
	}
}
