package qbft

import (
	"testing"

	"github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/qbft/spectest/tests/timeout"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"

	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance"
	"github.com/bloxapp/ssv/utils/logex"
)

// RunUponTimeoutSpecTest runs spec test type UponTimoutSpecTest.
func RunUponTimeoutSpecTest(t *testing.T, test *timeout.UponTimeoutSpecTest) {
	logger := logex.Build(test.Name, zapcore.DebugLevel, nil)
	forkVersion := forksprotocol.GenesisForkVersion
	identifier := []byte("id")
	qbftInstance := NewQbftInstance(logger, nil, nil, nil, nil, identifier, forkVersion).(*instance.Instance)

	var lastError error
	for _, r := range test.Rounds {
		// setup for test
		qbftInstance.GetState().ProposalAcceptedForCurrentRound.Store(&qbft.SignedMessage{})

		qbftInstance.UponChangeRoundTrigger()
		if err := qbftInstance.BroadcastChangeRound(); err != nil {
			lastError = err
		}

		require.EqualValues(t, r+1, qbftInstance.State.Round)
		require.Nil(t, qbftInstance.State.ProposalAcceptedForCurrentRound)
	}

	if len(test.ExpectedError) > 0 {
		require.EqualError(t, lastError, test.ExpectedError)
	} else {
		require.NoError(t, lastError)
	}
}
