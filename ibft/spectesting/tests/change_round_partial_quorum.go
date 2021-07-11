package tests

import (
	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/ibft/spectesting"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/msgqueue"
	"github.com/stretchr/testify/require"
	"testing"
)

// ChangeRoundPartialQuorum tests partial round change behaviour
type ChangeRoundPartialQuorum struct {
	instances  []*ibft.Instance
	inputValue []byte
	lambda     []byte
}

// Name returns test name
func (test *ChangeRoundPartialQuorum) Name() string {
	return "receive f+1 change round messages -> bump round -> set timer -> broadcast round change"
}

// Prepare prepares the test
func (test *ChangeRoundPartialQuorum) Prepare(t *testing.T) {
	test.lambda = []byte{1, 2, 3, 4}
	test.inputValue = spectesting.TestInputValue()

	test.instances = make([]*ibft.Instance, 0)
	for i, msgs := range test.MessagesSequence(t) {
		instance := spectesting.TestIBFTInstance(t, test.lambda)
		test.instances = append(test.instances, instance)
		instance.State.Round = uint64(i)

		// load messages to queue
		for _, msg := range msgs {
			instance.MsgQueue.AddMessage(&network.Message{
				SignedMessage: msg,
				Type:          network.NetworkMsg_IBFTType,
			})
		}
	}
}

// MessagesSequence includes all test messages
func (test *ChangeRoundPartialQuorum) MessagesSequence(t *testing.T) [][]*proto.SignedMessage {
	return [][]*proto.SignedMessage{
		{ // f+1 points to 2
			spectesting.ChangeRoundMsg(t, spectesting.TestSKs()[0], test.lambda, 2, 1),
			spectesting.ChangeRoundMsg(t, spectesting.TestSKs()[1], test.lambda, 2, 2),
		},
		{ // f+1 points to 3
			spectesting.ChangeRoundMsg(t, spectesting.TestSKs()[0], test.lambda, 0, 1),
			spectesting.ChangeRoundMsg(t, spectesting.TestSKs()[1], test.lambda, 0, 2),
			spectesting.ChangeRoundMsg(t, spectesting.TestSKs()[0], test.lambda, 3, 1),
			spectesting.ChangeRoundMsg(t, spectesting.TestSKs()[1], test.lambda, 3, 2),
		},
		{ // f+1 points to 4
			spectesting.ChangeRoundMsg(t, spectesting.TestSKs()[0], test.lambda, 0, 1),
			spectesting.ChangeRoundMsg(t, spectesting.TestSKs()[1], test.lambda, 0, 2),
			spectesting.ChangeRoundMsg(t, spectesting.TestSKs()[1], test.lambda, 4, 2),
			spectesting.ChangeRoundMsg(t, spectesting.TestSKs()[0], test.lambda, 10, 1),
			spectesting.ChangeRoundMsg(t, spectesting.TestSKs()[1], test.lambda, 10, 2),
			spectesting.ChangeRoundMsg(t, spectesting.TestSKs()[0], test.lambda, 5, 1),
			spectesting.ChangeRoundMsg(t, spectesting.TestSKs()[1], test.lambda, 6, 2),
			spectesting.ChangeRoundMsg(t, spectesting.TestSKs()[0], test.lambda, 4, 1),
		},
		{ // f+1 points not pointing anywhere
			spectesting.ChangeRoundMsg(t, spectesting.TestSKs()[0], test.lambda, 0, 1),
			spectesting.ChangeRoundMsg(t, spectesting.TestSKs()[1], test.lambda, 1, 2),
			spectesting.ChangeRoundMsg(t, spectesting.TestSKs()[0], test.lambda, 2, 1),
			spectesting.ChangeRoundMsg(t, spectesting.TestSKs()[1], test.lambda, 3, 2),
		},
		{ // f points to 2, no partial quorum
			spectesting.ChangeRoundMsg(t, spectesting.TestSKs()[0], test.lambda, 10, 1),
		},
	}
}

// Run runs the test
func (test *ChangeRoundPartialQuorum) Run(t *testing.T) {
	require.Len(t, test.instances, 5)

	spectesting.RequireReturnedTrueNoError(t, test.instances[0].ProcessChangeRoundPartialQuorum)
	require.EqualValues(t, 2, test.instances[0].State.Round)
	require.Len(t, test.instances[0].MsgQueue.MessagesForIndex(msgqueue.IBFTAllRoundChangeIndexKey(
		test.instances[0].State.Lambda,
		test.instances[0].State.SeqNumber)), 0)

	spectesting.RequireReturnedTrueNoError(t, test.instances[1].ProcessChangeRoundPartialQuorum)
	require.EqualValues(t, 3, test.instances[1].State.Round)
	require.Len(t, test.instances[1].MsgQueue.MessagesForIndex(msgqueue.IBFTAllRoundChangeIndexKey(
		test.instances[1].State.Lambda,
		test.instances[1].State.SeqNumber)), 0)

	spectesting.RequireReturnedTrueNoError(t, test.instances[2].ProcessChangeRoundPartialQuorum)
	require.EqualValues(t, 4, test.instances[2].State.Round)
	require.Len(t, test.instances[2].MsgQueue.MessagesForIndex(msgqueue.IBFTAllRoundChangeIndexKey(
		test.instances[2].State.Lambda,
		test.instances[2].State.SeqNumber)), 0)

	spectesting.RequireReturnedFalseNoError(t, test.instances[3].ProcessChangeRoundPartialQuorum)
	require.EqualValues(t, 3, test.instances[3].State.Round)
	require.Len(t, test.instances[3].MsgQueue.MessagesForIndex(msgqueue.IBFTAllRoundChangeIndexKey(
		test.instances[3].State.Lambda,
		test.instances[3].State.SeqNumber)), 4)

	spectesting.RequireReturnedFalseNoError(t, test.instances[4].ProcessChangeRoundPartialQuorum)
	require.EqualValues(t, 4, test.instances[4].State.Round)
	require.Len(t, test.instances[4].MsgQueue.MessagesForIndex(msgqueue.IBFTAllRoundChangeIndexKey(
		test.instances[4].State.Lambda,
		test.instances[4].State.SeqNumber)), 1)
}
