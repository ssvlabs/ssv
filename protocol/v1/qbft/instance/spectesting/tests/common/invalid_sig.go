package common

import (
	ibft2 "github.com/bloxapp/ssv/ibft/instance"
	"github.com/bloxapp/ssv/ibft/instance/spectesting"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"testing"
)

// InvalidSig tests processing msgs with wrong sig.
type InvalidSig struct {
	instance   *ibft2.Instance
	inputValue []byte
	lambda     []byte
}

// Name returns test name
func (test *InvalidSig) Name() string {
	return "Invalid sig messages"
}

// Prepare prepares the test
func (test *InvalidSig) Prepare(t *testing.T) {
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
func (test *InvalidSig) MessagesSequence(t *testing.T) []*proto.SignedMessage {
	return []*proto.SignedMessage{
		spectesting.PrePrepareMsg(t, spectesting.TestSKs()[1], test.lambda, test.inputValue, 1, 1),
		spectesting.PrepareMsg(t, spectesting.TestSKs()[1], test.lambda, test.inputValue, 1, 1),
		spectesting.CommitMsg(t, spectesting.TestSKs()[1], test.lambda, test.inputValue, 1, 1),
		spectesting.ChangeRoundMsg(t, spectesting.TestSKs()[1], test.lambda, 1, 1),
	}
}

// Run runs the test
func (test *InvalidSig) Run(t *testing.T) {
	spectesting.RequireReturnedTrueWithError(t, test.instance.ProcessMessage, "could not verify message signature")
	spectesting.RequireReturnedTrueWithError(t, test.instance.ProcessMessage, "could not verify message signature")
	spectesting.RequireReturnedTrueWithError(t, test.instance.ProcessMessage, "could not verify message signature")
	spectesting.RequireReturnedTrueWithError(t, test.instance.ProcessMessage, "could not verify message signature")
}
