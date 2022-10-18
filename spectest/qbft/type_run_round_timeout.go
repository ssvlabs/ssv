package qbft

import (
	"testing"

	"github.com/bloxapp/ssv-spec/qbft/spectest/tests/timeout"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"

	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance"
	"github.com/bloxapp/ssv/utils/logex"
)

// RunRoundTimeoutSpecTest runs spec test type RoundTimeoutSpecTest.
func RunRoundTimeoutSpecTest(t *testing.T, test *timeout.RoundTimeoutSpecTest) {
	require.Len(t, test.Rounds, len(test.ExpectedTimeout))

	logger := logex.Build(test.Name, zapcore.DebugLevel, nil)
	forkVersion := forksprotocol.GenesisForkVersion

	qbftInstance := NewQbftInstance(logger, nil, nil, nil, nil, nil, forkVersion)
	for i, r := range test.Rounds {
		require.EqualValues(t, test.ExpectedTimeout[i], qbftInstance.(*instance.Instance).TimeoutForRound(r))
	}
}
