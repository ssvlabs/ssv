package common

import (
	ibft2 "github.com/bloxapp/ssv/ibft/instance"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/ibft/spectesting"
	"github.com/bloxapp/ssv/network"
	"testing"
)

// WrongSequenceNumber tests processing msgs with wrong seq number.
type WrongSequenceNumber struct {
	instance   *ibft2.Instance
	inputValue []byte
	lambda     []byte
}

// Name returns test name
func (test *WrongSequenceNumber) Name() string {
	return "Wrong seq number"
}

// Prepare prepares the test
func (test *WrongSequenceNumber) Prepare(t *testing.T) {
	test.lambda = []byte{1, 2, 3, 4}
	test.inputValue = spectesting.TestInputValue()

	test.instance = spectesting.TestIBFTInstance(t, test.lambda)
	test.instance.State().Round.Set(1)
	test.instance.State().SeqNumber.Set(100)

	// load messages to queue
	for _, msg := range test.MessagesSequence(t) {
		test.instance.MsgQueue.AddMessage(&network.Message{
			SignedMessage: msg,
			Type:          network.NetworkMsg_IBFTType,
		})
	}
}

// MessagesSequence includes all messages
func (test *WrongSequenceNumber) MessagesSequence(t *testing.T) []*proto.SignedMessage {
	return []*proto.SignedMessage{
		spectesting.PrePrepareMsg(t, spectesting.TestSKs()[1], test.lambda, test.inputValue, 1, 1),
		spectesting.PrepareMsg(t, spectesting.TestSKs()[1], test.lambda, test.inputValue, 1, 1),
		spectesting.CommitMsg(t, spectesting.TestSKs()[1], test.lambda, test.inputValue, 1, 1),
		spectesting.ChangeRoundMsg(t, spectesting.TestSKs()[1], test.lambda, 1, 1),
	}
}

// Run runs the test
func (test *WrongSequenceNumber) Run(t *testing.T) {
	spectesting.RequireReturnedFalseNoError(t, test.instance.ProcessMessage)
	spectesting.RequireReturnedFalseNoError(t, test.instance.ProcessMessage)
	spectesting.RequireReturnedFalseNoError(t, test.instance.ProcessMessage)
	spectesting.RequireReturnedFalseNoError(t, test.instance.ProcessMessage)
}
