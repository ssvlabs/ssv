package preprepare

import (
	ibft2 "github.com/bloxapp/ssv/ibft/instance"
	"github.com/bloxapp/ssv/ibft/instance/spectesting"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/stretchr/testify/require"
	"testing"
)

// FuturePrePrepare tests a future pre-prepare msg (followed by prepare msg)
type FuturePrePrepare struct {
	instance   *ibft2.Instance
	inputValue []byte
	lambda     []byte
}

// Name returns test name
func (test *FuturePrePrepare) Name() string {
	return "future pre-prepare -> change rounds until future pre-prepare -> broadcast prepare"
}

// Prepare prepares the test
func (test *FuturePrePrepare) Prepare(t *testing.T) {
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

// MessagesSequence includes all messages
func (test *FuturePrePrepare) MessagesSequence(t *testing.T) []*proto.SignedMessage {
	return []*proto.SignedMessage{
		spectesting.PrePrepareMsg(t, spectesting.TestSKs()[0], test.lambda, test.inputValue, 2, 1),

		spectesting.PrepareMsg(t, spectesting.TestSKs()[2], test.lambda, test.inputValue, 2, 3),
		spectesting.PrepareMsg(t, spectesting.TestSKs()[3], test.lambda, test.inputValue, 2, 4),

		spectesting.ChangeRoundMsg(t, spectesting.TestSKs()[0], test.lambda, 2, 1),

		spectesting.PrepareMsg(t, spectesting.TestSKs()[0], test.lambda, test.inputValue, 2, 1),
	}
}

// Run runs the test
func (test *FuturePrePrepare) Run(t *testing.T) {
	// pre-prepare from future
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	// prepare from future
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)

	// change round
	spectesting.SimulateTimeout(test.instance, 2)
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)

	// broadcast prepare
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	require.EqualValues(t, test.inputValue, test.instance.State().PreparedValue.Get())
	require.EqualValues(t, 2, test.instance.State().PreparedRound.Get())
}
