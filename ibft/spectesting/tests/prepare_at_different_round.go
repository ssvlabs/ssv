package tests

import (
	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/ibft/spectesting"
	"github.com/bloxapp/ssv/network"
	"github.com/stretchr/testify/require"
	"testing"
)

// PrepareAtDifferentRound tests receiving a quorum of prepare msgs in a different round than state round.
type PrepareAtDifferentRound struct {
	instance   *ibft.Instance
	inputValue []byte
	lambda     []byte
	prevLambda []byte
}

// Name returns test name
func (test *PrepareAtDifferentRound) Name() string {
	return "pre-prepare -> prepare for round 5 -> change round until round 5 -> commit"
}

// Prepare prepares the test
func (test *PrepareAtDifferentRound) Prepare(t *testing.T) {
	test.lambda = []byte{1, 2, 3, 4}
	test.prevLambda = []byte{0, 0, 0, 0}
	test.inputValue = spectesting.TestInputValue()

	test.instance = spectesting.TestIBFTInstance(t, test.lambda, test.prevLambda)
	test.instance.State.Round = 1

	// load messages to queue
	for _, msg := range test.MessagesSequence(t) {
		test.instance.MsgQueue.AddMessage(&network.Message{
			Lambda:        test.lambda,
			SignedMessage: msg,
			Type:          network.NetworkMsg_IBFTType,
		})
	}
}

// MessagesSequence includes all test messages
func (test *PrepareAtDifferentRound) MessagesSequence(t *testing.T) []*proto.SignedMessage {
	return []*proto.SignedMessage{
		spectesting.PrePrepareMsg(t, spectesting.TestSKs()[0], test.lambda, test.prevLambda, test.inputValue, 1, 1),

		spectesting.PrepareMsg(t, spectesting.TestSKs()[0], test.lambda, test.prevLambda, test.inputValue, 5, 1),
		spectesting.PrepareMsg(t, spectesting.TestSKs()[1], test.lambda, test.prevLambda, test.inputValue, 5, 2),
		spectesting.PrepareMsg(t, spectesting.TestSKs()[2], test.lambda, test.prevLambda, test.inputValue, 5, 3),
		spectesting.PrepareMsg(t, spectesting.TestSKs()[3], test.lambda, test.prevLambda, test.inputValue, 5, 4),

		spectesting.CommitMsg(t, spectesting.TestSKs()[0], test.lambda, test.prevLambda, test.inputValue, 5, 1),
		spectesting.CommitMsg(t, spectesting.TestSKs()[1], test.lambda, test.prevLambda, test.inputValue, 5, 2),
		spectesting.CommitMsg(t, spectesting.TestSKs()[2], test.lambda, test.prevLambda, test.inputValue, 5, 3),
		spectesting.CommitMsg(t, spectesting.TestSKs()[3], test.lambda, test.prevLambda, test.inputValue, 5, 4),
	}
}

// Run runs the test
func (test *PrepareAtDifferentRound) Run(t *testing.T) {
	// pre-prepare
	spectesting.RequireProcessedMessage(t, test.instance.ProcessMessage)

	// simulate a later round prepare and how the node gets there in terms of change round timeouts
	for i := 2; i <= 5; i++ {
		spectesting.RequireNotProcessedMessage(t, test.instance.ProcessMessage)
		spectesting.SimulateTimeout(test.instance, uint64(i))
	}

	// non qualified prepare quorum
	spectesting.RequireProcessedMessage(t, test.instance.ProcessMessage)
	quorum, _ := test.instance.PrepareMessages.QuorumAchieved(5, test.inputValue)
	require.False(t, quorum)
	// qualified prepare quorum
	spectesting.RequireProcessedMessage(t, test.instance.ProcessMessage)
	spectesting.RequireProcessedMessage(t, test.instance.ProcessMessage)
	spectesting.RequireProcessedMessage(t, test.instance.ProcessMessage)
	quorum, _ = test.instance.PrepareMessages.QuorumAchieved(5, test.inputValue)
	require.True(t, quorum)
	// non qualified commit quorum
	spectesting.RequireProcessedMessage(t, test.instance.ProcessMessage)
	quorum, _ = test.instance.CommitMessages.QuorumAchieved(5, test.inputValue)
	require.False(t, quorum)
	// qualified commit quorum
	spectesting.RequireProcessedMessage(t, test.instance.ProcessMessage)
	spectesting.RequireProcessedMessage(t, test.instance.ProcessMessage)
	spectesting.RequireNotProcessedMessage(t, test.instance.ProcessMessage) // we purge all messages after decided was reached
	quorum, _ = test.instance.CommitMessages.QuorumAchieved(5, test.inputValue)
	require.True(t, quorum)

	require.EqualValues(t, proto.RoundState_Decided, test.instance.State.Stage)
}
