package changeround

import (
	ibft2 "github.com/bloxapp/ssv/ibft/instance"
	"github.com/bloxapp/ssv/ibft/instance/spectesting"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/stretchr/testify/require"
	"testing"
)

// ChangeToRound2AndDecide tests coming to consensus after a non prepared change round
type ChangeToRound2AndDecide struct {
	instance   *ibft2.Instance
	inputValue []byte
	lambda     []byte
}

// Name returns test name
func (test *ChangeToRound2AndDecide) Name() string {
	return "pre-prepare -> change round -> pre-prepare -> prepare -> decide"
}

// Prepare prepares the test
func (test *ChangeToRound2AndDecide) Prepare(t *testing.T) {
	test.lambda = []byte{1, 2, 3, 4}
	test.inputValue = spectesting.TestInputValue()

	test.instance = spectesting.TestIBFTInstance(t, test.lambda)
	test.instance.State().Round.Set(1)

	// load messages to queue
	for _, msg := range test.MessagesSequence(t) {
		test.instance.MsgQueue.AddMessage(&network.Message{
			SignedMessage: msg,
			Type:          network.NetworkMsg_IBFTType,
		})
	}
}

// MessagesSequence includes all test messages
func (test *ChangeToRound2AndDecide) MessagesSequence(t *testing.T) []*proto.SignedMessage {
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
func (test *ChangeToRound2AndDecide) Run(t *testing.T) {
	// pre-prepare
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	spectesting.SimulateTimeout(test.instance, 2)

	// change round
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	err := test.instance.JustifyRoundChange(2)
	require.NoError(t, err)

	// check pre-prepare justification
	err = test.instance.JustifyPrePrepare(2, nil)
	require.NoError(t, err)

	// process all messages
	for {
		if res, _ := test.instance.ProcessMessage(); !res {
			break
		}
	}
	require.EqualValues(t, proto.RoundState_Decided, test.instance.State().Stage.Get())
}
