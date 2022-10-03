package qbft

import (
	"testing"

	spectests "github.com/bloxapp/ssv-spec/qbft/spectest/tests"
	"github.com/stretchr/testify/require"

	qbftprotocol "github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/leader/roundrobin"
)

// RunRoundRobinSpecTest for spec test type RoundRobinSpecTest.
func RunRoundRobinSpecTest(t *testing.T, test *spectests.RoundRobinSpecTest) {
	require.True(t, len(test.Heights) > 0)

	share, _ := ToMappedShare(t, test.Share)

	for i, h := range test.Heights {
		s := &qbftprotocol.State{}
		s.Round.Store(test.Rounds[i])
		s.Height.Store(h)

		proposer := roundrobin.New(share, s)
		require.EqualValues(t, test.Proposers[i], proposer.Calculate(uint64(test.Rounds[i])))
	}
}
