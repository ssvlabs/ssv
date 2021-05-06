package tests

import (
	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/ibft/spectesting"
	"github.com/bloxapp/ssv/network"
	"github.com/stretchr/testify/require"
	"testing"
)

// PrepareChangeRoundAndDecide tests coming to consensus after preparing and then changing round.
type PrepareChangeRoundAndDecide struct {
	instance   *ibft.Instance
	inputValue []byte
	lambda     []byte
	prevLambda []byte
}

// Name returns test name
func (test *PrepareChangeRoundAndDecide) Name() string {
	return "pre-prepare -> prepare -> change round -> prepare -> decide"
}

// Prepare prepares the test
func (test *PrepareChangeRoundAndDecide) Prepare(t *testing.T) {
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

// MessagesSequence includes test messages
func (test *PrepareChangeRoundAndDecide) MessagesSequence(t *testing.T) []*proto.SignedMessage {
	signersMap := map[uint64][]byte{
		1: spectesting.TestSKs()[0],
		2: spectesting.TestSKs()[1],
		3: spectesting.TestSKs()[2],
		4: spectesting.TestSKs()[3],
	}

	return []*proto.SignedMessage{
		spectesting.PrePrepareMsg(t, spectesting.TestSKs()[0], test.lambda, test.prevLambda, test.inputValue, 1, 1),

		spectesting.PrepareMsg(t, spectesting.TestSKs()[0], test.lambda, test.prevLambda, test.inputValue, 1, 1),
		spectesting.PrepareMsg(t, spectesting.TestSKs()[1], test.lambda, test.prevLambda, test.inputValue, 1, 2),
		spectesting.PrepareMsg(t, spectesting.TestSKs()[2], test.lambda, test.prevLambda, test.inputValue, 1, 3),
		spectesting.PrepareMsg(t, spectesting.TestSKs()[3], test.lambda, test.prevLambda, test.inputValue, 1, 4),

		spectesting.ChangeRoundMsgWithPrepared(t, spectesting.TestSKs()[0], test.lambda, test.prevLambda, test.inputValue, signersMap, 2, 1, 1),
		spectesting.ChangeRoundMsgWithPrepared(t, spectesting.TestSKs()[1], test.lambda, test.prevLambda, test.inputValue, signersMap, 2, 1, 2),
		spectesting.ChangeRoundMsgWithPrepared(t, spectesting.TestSKs()[2], test.lambda, test.prevLambda, test.inputValue, signersMap, 2, 1, 3),
		spectesting.ChangeRoundMsgWithPrepared(t, spectesting.TestSKs()[3], test.lambda, test.prevLambda, test.inputValue, signersMap, 2, 1, 4),

		spectesting.PrePrepareMsg(t, spectesting.TestSKs()[0], test.lambda, test.prevLambda, test.inputValue, 2, 1),

		spectesting.PrepareMsg(t, spectesting.TestSKs()[0], test.lambda, test.prevLambda, test.inputValue, 2, 1),
		spectesting.PrepareMsg(t, spectesting.TestSKs()[1], test.lambda, test.prevLambda, test.inputValue, 2, 2),
		spectesting.PrepareMsg(t, spectesting.TestSKs()[2], test.lambda, test.prevLambda, test.inputValue, 2, 3),
		spectesting.PrepareMsg(t, spectesting.TestSKs()[3], test.lambda, test.prevLambda, test.inputValue, 2, 4),

		spectesting.CommitMsg(t, spectesting.TestSKs()[0], test.lambda, test.prevLambda, test.inputValue, 2, 1),
		spectesting.CommitMsg(t, spectesting.TestSKs()[1], test.lambda, test.prevLambda, test.inputValue, 2, 2),
		spectesting.CommitMsg(t, spectesting.TestSKs()[2], test.lambda, test.prevLambda, test.inputValue, 2, 3),
		spectesting.CommitMsg(t, spectesting.TestSKs()[3], test.lambda, test.prevLambda, test.inputValue, 2, 4),
	}
}

// Run runs the test
func (test *PrepareChangeRoundAndDecide) Run(t *testing.T) {
	// pre-prepare
	spectesting.RequireProcessedMessage(t, test.instance.ProcessMessage)

	// prepare
	spectesting.RequireProcessedMessage(t, test.instance.ProcessMessage)
	spectesting.RequireProcessedMessage(t, test.instance.ProcessMessage)
	spectesting.RequireProcessedMessage(t, test.instance.ProcessMessage)
	spectesting.RequireProcessedMessage(t, test.instance.ProcessMessage)
	spectesting.SimulateTimeout(test.instance, 2)

	// change round
	spectesting.RequireProcessedMessage(t, test.instance.ProcessMessage)
	spectesting.RequireProcessedMessage(t, test.instance.ProcessMessage)
	spectesting.RequireProcessedMessage(t, test.instance.ProcessMessage)
	spectesting.RequireProcessedMessage(t, test.instance.ProcessMessage)
	justified, err := test.instance.JustifyRoundChange(2)
	require.NoError(t, err)
	require.True(t, justified)

	// justify pre-prepare
	justified, err = test.instance.JustifyPrePrepare(2)
	require.NoError(t, err)
	require.True(t, justified)
	spectesting.RequireProcessedMessage(t, test.instance.ProcessMessage)

	// process all messages
	for {
		if res, _ := test.instance.ProcessMessage(); !res {
			break
		}
	}
	require.EqualValues(t, proto.RoundState_Decided, test.instance.State.Stage)
}
