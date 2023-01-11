package queue

import (
	"fmt"
	"testing"

	"github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

func TestPriorityQueuePushAndPop(t *testing.T) {
	mockState := &State{
		HasRunningInstance: true,
		Height:             100,
		Slot:               64,
		Quorum:             4,
	}
	prioritizer := NewMessagePrioritizer(mockState)
	queue := New(prioritizer)

	// Push 2 messages.
	msg := decodeAndPush(t, queue, mockConsensusMessage{Height: 100, Type: qbft.PrepareMsgType}, mockState)
	require.Equal(t, 1, queue.Len())
	msg2 := decodeAndPush(t, queue, mockConsensusMessage{Height: 101, Type: qbft.PrepareMsgType}, mockState)
	require.Equal(t, 2, queue.Len())

	// Pop non-existing BeaconRole.
	popped := queue.Pop(FilterRole(types.BNRoleProposer))
	require.Nil(t, popped)

	// Pop 1st message.
	popped, pop := queue.Peek(FilterRole(msg.MsgID.GetRoleType()))
	require.Equal(t, 2, queue.Len())
	pop()
	require.Equal(t, 1, queue.Len())
	require.Equal(t, msg, popped)

	// Pop 2nd message.
	popped = queue.Pop(FilterRole(msg.MsgID.GetRoleType()))
	require.Equal(t, 0, queue.Len())
	require.Equal(t, msg2, popped)

	// Pop nil.
	popped = queue.Pop(FilterRole(msg.MsgID.GetRoleType()))
	require.Nil(t, popped)
}

// TestPriorityQueueOrder tests that the queue returns the messages in the correct order.
func TestPriorityQueueOrder(t *testing.T) {
	for _, test := range messagePriorityTests {
		t.Run(fmt.Sprintf("PriorityQueue: %s", test.name), func(t *testing.T) {
			// Create the PriorityQueue and populate it with messages.
			q := New(NewMessagePrioritizer(test.state))

			decodedMessages := make([]*DecodedSSVMessage, len(test.messages))
			for i, m := range test.messages {
				mm, err := DecodeSSVMessage(m.ssvMessage(test.state))
				require.NoError(t, err)

				q.Push(mm)

				// Keep track of the messages we push so we can
				// effortlessly compare to them later.
				decodedMessages[i] = mm
			}

			// Pop messages from the queue and compare to the expected order.
			for i, excepted := range decodedMessages {
				actual := q.Pop(nil)
				require.Equal(t, excepted, actual, "incorrect message at index %d", i)
			}
		})
	}
}

func BenchmarkPriorityQueueConcurrent(b *testing.B) {
	mockState := &State{
		HasRunningInstance: true,
		Height:             100,
		Slot:               64,
		Quorum:             4,
	}
	prioritizer := NewMessagePrioritizer(mockState)
	queue := New(prioritizer)

	decoded, err := DecodeSSVMessage(mockConsensusMessage{Height: 101, Type: qbft.PrepareMsgType}.ssvMessage(mockState))
	require.NoError(b, err)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			queue.Push(decoded)
			queue.Pop(FilterRole(types.BNRoleProposer))
		}
	})
}

func decodeAndPush(t require.TestingT, queue Queue, msg mockMessage, state *State) *DecodedSSVMessage {
	decoded, err := DecodeSSVMessage(msg.ssvMessage(state))
	require.NoError(t, err)
	queue.Push(decoded)
	return decoded
}
