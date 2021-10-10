package preprepare

import (
	ibft2 "github.com/bloxapp/ssv/ibft/instance"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/ibft/spectesting"
	"github.com/bloxapp/ssv/network"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"testing"
)

// NonJustifiedPrePrepapre2 tests coming to consensus after a non prepared change round
type NonJustifiedPrePrepapre2 struct {
	instance   *ibft2.Instance
	inputValue []byte
	lambda     []byte
}

// Name returns test name
func (test *NonJustifiedPrePrepapre2) Name() string {
	return "pre-prepare -> prepare -> simulate round timeout -> change round quorum -> unjustified pre-prepare (wrong input value)"
}

// Prepare prepares the test
func (test *NonJustifiedPrePrepapre2) Prepare(t *testing.T) {
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
func (test *NonJustifiedPrePrepapre2) MessagesSequence(t *testing.T) []*proto.SignedMessage {
	signersMap := map[uint64]*bls.SecretKey{
		1: spectesting.TestSKs()[0],
		2: spectesting.TestSKs()[1],
		3: spectesting.TestSKs()[2],
		4: spectesting.TestSKs()[3],
	}

	return []*proto.SignedMessage{
		spectesting.PrePrepareMsg(t, spectesting.TestSKs()[0], test.lambda, test.inputValue, 1, 1),

		spectesting.PrepareMsg(t, spectesting.TestSKs()[0], test.lambda, test.inputValue, 1, 1),
		spectesting.PrepareMsg(t, spectesting.TestSKs()[1], test.lambda, test.inputValue, 1, 2),
		spectesting.PrepareMsg(t, spectesting.TestSKs()[2], test.lambda, test.inputValue, 1, 3),
		spectesting.PrepareMsg(t, spectesting.TestSKs()[3], test.lambda, test.inputValue, 1, 4),

		spectesting.ChangeRoundMsgWithPrepared(t, spectesting.TestSKs()[0], test.lambda, test.inputValue, signersMap, 2, 1, 1),
		spectesting.ChangeRoundMsg(t, spectesting.TestSKs()[1], test.lambda, 2, 2),
		spectesting.ChangeRoundMsg(t, spectesting.TestSKs()[2], test.lambda, 2, 3),
		spectesting.ChangeRoundMsg(t, spectesting.TestSKs()[3], test.lambda, 2, 4),

		spectesting.PrePrepareMsg(t, spectesting.TestSKs()[0], test.lambda, []byte("wrong value"), 2, 1),
	}
}

// Run runs the test
func (test *NonJustifiedPrePrepapre2) Run(t *testing.T) {
	// pre-prepare
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)

	// prepared
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	require.EqualValues(t, test.inputValue, test.instance.State().PreparedValue.Get())
	require.EqualValues(t, 1, test.instance.State().PreparedRound.Get())

	// change round
	spectesting.SimulateTimeout(test.instance, 2)
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	require.EqualValues(t, 2, test.instance.State().Round.Get())

	// try to broadcast unjustified pre-prepare
	spectesting.RequireReturnedTrueWithError(t, test.instance.ProcessMessage, "Unjustified pre-prepare: preparedValue different than highest prepared")
}
