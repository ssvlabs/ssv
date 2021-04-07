package tests

import (
	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/ibft/spectesting"
	"github.com/bloxapp/ssv/network"
	"github.com/stretchr/testify/require"
	"testing"
)

// PrepareAtDifferentRound is a simple happy flow of iBFT
type ValidSimpleRun struct {
	instance   *ibft.Instance
	inputValue []byte
	lambda     []byte
	prevLambda []byte
}

func (test *ValidSimpleRun) Name() string {
	return "Valid simple test"
}

func (test *ValidSimpleRun) Prepare(t *testing.T) {
	test.lambda = []byte{1, 2, 3, 4}
	test.prevLambda = []byte{0, 0, 0, 0}
	test.inputValue = spectesting.TestInputValue()

	test.instance = spectesting.TestIBFTInstance(t, test.lambda, test.prevLambda)
	test.instance.State.Round = 1

	// load messages to queue
	for _, msg := range test.MessagesSequence(t) {
		test.instance.MsgQueue.AddMessage(&network.Message{
			Lambda: test.lambda,
			Msg:    msg,
			Type:   network.IBFTBroadcastingType,
		})
	}
}

func (test *ValidSimpleRun) MessagesSequence(t *testing.T) []*proto.SignedMessage {
	return []*proto.SignedMessage{
		spectesting.PrePrepareMsg(t, spectesting.TestSKs()[0], test.lambda, test.prevLambda, test.inputValue, 1, 1),

		spectesting.PrepareMsg(t, spectesting.TestSKs()[0], test.lambda, test.prevLambda, test.inputValue, 1, 1),
		spectesting.PrepareMsg(t, spectesting.TestSKs()[1], test.lambda, test.prevLambda, test.inputValue, 1, 2),
		spectesting.PrepareMsg(t, spectesting.TestSKs()[2], test.lambda, test.prevLambda, test.inputValue, 1, 3),
		spectesting.PrepareMsg(t, spectesting.TestSKs()[3], test.lambda, test.prevLambda, test.inputValue, 1, 4),

		spectesting.CommitMsg(t, spectesting.TestSKs()[0], test.lambda, test.prevLambda, test.inputValue, 1, 1),
		spectesting.CommitMsg(t, spectesting.TestSKs()[1], test.lambda, test.prevLambda, test.inputValue, 1, 2),
		spectesting.CommitMsg(t, spectesting.TestSKs()[2], test.lambda, test.prevLambda, test.inputValue, 1, 3),
		spectesting.CommitMsg(t, spectesting.TestSKs()[3], test.lambda, test.prevLambda, test.inputValue, 1, 4),
	}
}

func (test *ValidSimpleRun) Run(t *testing.T) {
	// pre-prepare
	require.True(t, test.instance.ProcessMessage())
	// non qualified prepare quorum
	require.True(t, test.instance.ProcessMessage())
	quorum, _ := test.instance.PrepareMessages.QuorumAchieved(1, test.inputValue)
	require.False(t, quorum)
	// qualified prepare quorum
	require.True(t, test.instance.ProcessMessage())
	require.True(t, test.instance.ProcessMessage())
	require.True(t, test.instance.ProcessMessage())
	quorum, _ = test.instance.PrepareMessages.QuorumAchieved(1, test.inputValue)
	require.True(t, quorum)
	// non qualified commit quorum
	require.True(t, test.instance.ProcessMessage())
	quorum, _ = test.instance.CommitMessages.QuorumAchieved(1, test.inputValue)
	require.False(t, quorum)
	// qualified commit quorum
	require.True(t, test.instance.ProcessMessage())
	require.True(t, test.instance.ProcessMessage())
	require.False(t, test.instance.ProcessMessage()) // we purge all messages after decided was reached
	quorum, _ = test.instance.CommitMessages.QuorumAchieved(1, test.inputValue)
	require.True(t, quorum)

	require.EqualValues(t, proto.RoundState_Decided, test.instance.State.Stage)
}
