package tests

import (
	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/ibft/spectesting"
	"github.com/bloxapp/ssv/network"
	"github.com/stretchr/testify/require"
	"testing"
)

// ChangeRoundAndDecide tests coming to consensus after a non prepared change round
type ChangeRoundAndDecide struct {
	instance   *ibft.Instance
	inputValue []byte
	lambda     []byte
}

// Name returns test name
func (test *ChangeRoundAndDecide) Name() string {
	return "pre-prepare -> change round -> pre-prepare -> prepare -> decide"
}

// Prepare prepares the test
func (test *ChangeRoundAndDecide) Prepare(t *testing.T) {
	test.lambda = []byte{1, 2, 3, 4}
	test.inputValue = spectesting.TestInputValue()

	test.instance = spectesting.TestIBFTInstance(t, test.lambda)
	test.instance.State.Round = 1

	// load messages to queue
	for _, msg := range test.MessagesSequence(t) {
		test.instance.MsgQueue.AddMessage(&network.Message{
			SignedMessage: msg,
			Type:          network.NetworkMsg_IBFTType,
		})
	}
}

// MessagesSequence includes all test messages
func (test *ChangeRoundAndDecide) MessagesSequence(t *testing.T) []*proto.SignedMessage {
	return []*proto.SignedMessage{
		spectesting.PrePrepareMsg(t, spectesting.TestSKs()[0], test.lambda, test.inputValue, 1, 1),

		spectesting.ChangeRoundMsg(t, spectesting.TestSKs()[0], test.lambda, 2, 1),
		spectesting.ChangeRoundMsg(t, spectesting.TestSKs()[1], test.lambda, 2, 2),
		spectesting.ChangeRoundMsg(t, spectesting.TestSKs()[2], test.lambda, 2, 3),
		spectesting.ChangeRoundMsg(t, spectesting.TestSKs()[3], test.lambda, 2, 4),

		spectesting.PrePrepareMsg(t, spectesting.TestSKs()[0], test.lambda, test.inputValue, 2, 1),

		spectesting.PrepareMsg(t, spectesting.TestSKs()[0], test.lambda, test.inputValue, 2, 1),
		spectesting.PrepareMsg(t, spectesting.TestSKs()[1], test.lambda, test.inputValue, 2, 2),
		spectesting.PrepareMsg(t, spectesting.TestSKs()[2], test.lambda, test.inputValue, 2, 3),
		spectesting.PrepareMsg(t, spectesting.TestSKs()[3], test.lambda, test.inputValue, 2, 4),

		spectesting.CommitMsg(t, spectesting.TestSKs()[0], test.lambda, test.inputValue, 2, 1),
		spectesting.CommitMsg(t, spectesting.TestSKs()[1], test.lambda, test.inputValue, 2, 2),
		spectesting.CommitMsg(t, spectesting.TestSKs()[2], test.lambda, test.inputValue, 2, 3),
		spectesting.CommitMsg(t, spectesting.TestSKs()[3], test.lambda, test.inputValue, 2, 4),
	}
}

// Run runs the test
func (test *ChangeRoundAndDecide) Run(t *testing.T) {
	// pre-prepare
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	spectesting.SimulateTimeout(test.instance, 2)

	// change round
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	justified, err := test.instance.JustifyRoundChange(2)
	require.NoError(t, err)
	require.True(t, justified)

	// check pre-prepare justification
	err = test.instance.JustifyPrePrepare(2)
	require.NoError(t, err)

	// process all messages
	for {
		if res, _ := test.instance.ProcessMessage(); !res {
			break
		}
	}
	require.EqualValues(t, proto.RoundState_Decided, test.instance.State.Stage)
}
