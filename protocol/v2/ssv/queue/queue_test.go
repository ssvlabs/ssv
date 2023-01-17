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
	queue := New()

	require.True(t, queue.IsEmpty())

	// Push 2 messages.
	msg := decodeAndPush(t, queue, mockConsensusMessage{Height: 100, Type: qbft.PrepareMsgType}, mockState)
	msg2 := decodeAndPush(t, queue, mockConsensusMessage{Height: 101, Type: qbft.PrepareMsgType}, mockState)
	require.False(t, queue.IsEmpty())

	// Pop 1st message.
	popped := queue.Pop(NewMessagePrioritizer(mockState), FilterRole(msg.MsgID.GetRoleType()))
	//require.False(t, queue.IsEmpty())
	require.Equal(t, msg, popped)

	// Pop 2nd message.
	popped = queue.Pop(NewMessagePrioritizer(mockState), FilterRole(msg.MsgID.GetRoleType()))
	require.True(t, queue.IsEmpty())
	require.NotNil(t, popped)
	require.Equal(t, msg2, popped)

	// Pop nil.
	popped = queue.Pop(NewMessagePrioritizer(mockState), FilterRole(msg.MsgID.GetRoleType()))
	require.Nil(t, popped)
}

// TestPriorityQueueOrder tests that the queue returns the messages in the correct order.
func TestPriorityQueueOrder(t *testing.T) {
	for _, test := range messagePriorityTests {
		t.Run(fmt.Sprintf("PriorityQueue: %s", test.name), func(t *testing.T) {
			// Create the PriorityQueue and populate it with messages.
			q := New()

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
				actual := q.Pop(NewMessagePrioritizer(test.state), nil)
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
	queue := New()

	decoded, err := DecodeSSVMessage(mockConsensusMessage{Height: 101, Type: qbft.PrepareMsgType}.ssvMessage(mockState))
	require.NoError(b, err)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			queue.Push(decoded)
			queue.Pop(prioritizer, FilterRole(types.BNRoleProposer))
		}
	})
}

func decodeAndPush(t require.TestingT, queue Queue, msg mockMessage, state *State) *DecodedSSVMessage {
	decoded, err := DecodeSSVMessage(msg.ssvMessage(state))
	require.NoError(t, err)
	queue.Push(decoded)
	return decoded
}
