package preprepare

import (
	ibft2 "github.com/bloxapp/ssv/ibft/instance"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/ibft/spectesting"
	"github.com/bloxapp/ssv/network"
	"testing"
)

// InvalidPrePrepareValue tests invalid pre-prepare value
type InvalidPrePrepareValue struct {
	instance   *ibft2.Instance
	inputValue []byte
	lambda     []byte
}

// Name returns test name
func (test *InvalidPrePrepareValue) Name() string {
	return "pre-prepare (invalid value)"
}

// Prepare prepares the test
func (test *InvalidPrePrepareValue) Prepare(t *testing.T) {
	test.lambda = []byte{1, 2, 3, 4}
	test.inputValue = spectesting.InvalidTestInputValue()

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
func (test *InvalidPrePrepareValue) MessagesSequence(t *testing.T) []*proto.SignedMessage {
	return []*proto.SignedMessage{
		spectesting.PrePrepareMsg(t, spectesting.TestSKs()[0], test.lambda, test.inputValue, 1, 1),
	}
}

// Run runs the test
func (test *InvalidPrePrepareValue) Run(t *testing.T) {
	// pre-prepare
	spectesting.RequireReturnedTrueWithError(t, test.instance.ProcessMessage, "failed while validating pre-prepare: msg value is wrong")
}
