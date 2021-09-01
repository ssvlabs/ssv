package tests

import (
	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/ibft/spectesting"
	"github.com/bloxapp/ssv/network"
	"github.com/stretchr/testify/require"
	"testing"
)

// PrevRoundDecided tests a delayed commit message from previous round arrives and decides the instance
type PrevRoundDecided struct {
	instance   *ibft.Instance
	inputValue []byte
	lambda     []byte
}

// Name returns test name
func (test *PrevRoundDecided) Name() string {
	return "previous round arrives and decides the instance"
}

// Prepare prepares the test
func (test *PrevRoundDecided) Prepare(t *testing.T) {
	test.lambda = []byte{1, 2, 3, 4}
	test.inputValue = spectesting.TestInputValue()

	test.instance = spectesting.TestIBFTInstance(t, test.lambda)
	test.instance.State.Round.Set(1)

	// load messages to queue
	for _, msg := range test.MessagesSequence(t) {
		test.instance.MsgQueue.AddMessage(&network.Message{
			SignedMessage: msg,
			Type:          network.NetworkMsg_IBFTType,
		})
	}
}

// MessagesSequence includes all messages
func (test *PrevRoundDecided) MessagesSequence(t *testing.T) []*proto.SignedMessage {
	return []*proto.SignedMessage{
		spectesting.PrePrepareMsg(t, spectesting.TestSKs()[0], test.lambda, test.inputValue, 1, 1),

		spectesting.PrepareMsg(t, spectesting.TestSKs()[0], test.lambda, test.inputValue, 1, 1),
		spectesting.PrepareMsg(t, spectesting.TestSKs()[1], test.lambda, test.inputValue, 1, 2),
		spectesting.PrepareMsg(t, spectesting.TestSKs()[2], test.lambda, test.inputValue, 1, 3),

		spectesting.CommitMsg(t, spectesting.TestSKs()[0], test.lambda, test.inputValue, 1, 1),
		spectesting.CommitMsg(t, spectesting.TestSKs()[1], test.lambda, test.inputValue, 1, 2),

		spectesting.ChangeRoundMsg(t, spectesting.TestSKs()[0], test.lambda, 2, 1),
		spectesting.ChangeRoundMsg(t, spectesting.TestSKs()[1], test.lambda, 2, 2),
		spectesting.ChangeRoundMsg(t, spectesting.TestSKs()[2], test.lambda, 2, 3),
	}
}

// Run runs the test
func (test *PrevRoundDecided) Run(t *testing.T) {
	// pre-prepare
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)

	// prepare
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	quorum, _ := test.instance.PrepareMessages.QuorumAchieved(1, test.inputValue)
	require.True(t, quorum)

	// non qualified commit quorum
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	quorum, _ = test.instance.CommitMessages.QuorumAchieved(1, test.inputValue)
	require.False(t, quorum)

	// simulate timeout
	spectesting.SimulateTimeout(test.instance, 2)

	// change round quorum
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	require.EqualValues(t, 2, test.instance.State.Round.Get())

	// receive last commit message for quorum
	test.instance.MsgQueue.AddMessage(&network.Message{
		SignedMessage: spectesting.CommitMsg(t, spectesting.TestSKs()[2], test.lambda, test.inputValue, 1, 3),
		Type:          network.NetworkMsg_IBFTType,
	})
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	require.EqualValues(t, proto.RoundState_Decided, test.instance.State.Stage.Get())
}
