package changeround

import (
	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/ibft/spectesting"
	"github.com/bloxapp/ssv/network"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"testing"
)

// NotPreparedError tests a pre-prepare following a change round with justification of a previously prepared round
type PreparedFollowedByPrePrepared struct {
	instance   *ibft.Instance
	inputValue []byte
	lambda     []byte
}

// Name returns test name
func (test *PreparedFollowedByPrePrepared) Name() string {
	return "pre-prepare -> change round (with justification for prepared) -> pre-prepare with change round justification"
}

// Prepare prepares the test
func (test *PreparedFollowedByPrePrepared) Prepare(t *testing.T) {
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

// MessagesSequence includes all test messages
func (test *PreparedFollowedByPrePrepared) MessagesSequence(t *testing.T) []*proto.SignedMessage {
	signersMap := map[uint64]*bls.SecretKey{
		1: spectesting.TestSKs()[0],
		2: spectesting.TestSKs()[1],
		3: spectesting.TestSKs()[2],
		4: spectesting.TestSKs()[3],
	}

	return []*proto.SignedMessage{
		spectesting.PrePrepareMsg(t, spectesting.TestSKs()[0], test.lambda, test.inputValue, 1, 1),

		spectesting.ChangeRoundMsgWithPrepared(t, spectesting.TestSKs()[1], test.lambda, test.inputValue, signersMap, 4, 3, 2),
		spectesting.ChangeRoundMsg(t, spectesting.TestSKs()[2], test.lambda, 4, 3),
		spectesting.ChangeRoundMsg(t, spectesting.TestSKs()[3], test.lambda, 4, 4),
	}
}

// Run runs the test
func (test *PreparedFollowedByPrePrepared) Run(t *testing.T) {
	// pre-prepare
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)

	// change round to round 4
	spectesting.SimulateTimeout(test.instance, 2)
	spectesting.SimulateTimeout(test.instance, 3)
	spectesting.SimulateTimeout(test.instance, 4)

	// change rounds
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)

	require.EqualError(t, test.instance.JustifyPrePrepare(4, nil), "unjustified change round for pre-prepare, value different than highest prepared")
	notPrepared, highest, err := test.instance.HighestPrepared(4)
	require.NoError(t, err)
	require.False(t, notPrepared)
	require.EqualValues(t, test.inputValue, highest.PreparedValue)
}
