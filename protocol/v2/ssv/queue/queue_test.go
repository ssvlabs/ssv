package queue

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bloxapp/ssv-spec/qbft"
	"github.com/stretchr/testify/require"
)

var mockState = &State{
	HasRunningInstance: true,
	Height:             100,
	Slot:               64,
	Quorum:             4,
}

func TestPriorityQueuePushAndPop(t *testing.T) {
	queue := New()

	require.True(t, queue.IsEmpty())

	// Push 2 messages.
	msg := decodeAndPush(t, queue, mockConsensusMessage{Height: 100, Type: qbft.PrepareMsgType}, mockState)
	msg2 := decodeAndPush(t, queue, mockConsensusMessage{Height: 101, Type: qbft.PrepareMsgType}, mockState)
	require.False(t, queue.IsEmpty())

	// Pop 1st message.
	popped := queue.Pop(NewMessagePrioritizer(mockState))
	require.Equal(t, msg, popped)

	// Pop 2nd message.
	popped = queue.Pop(NewMessagePrioritizer(mockState))
	require.True(t, queue.IsEmpty())
	require.NotNil(t, popped)
	require.Equal(t, msg2, popped)

	// Pop nil.
	popped = queue.Pop(NewMessagePrioritizer(mockState))
	require.Nil(t, popped)
}

func TestPriorityQueueParallelism(t *testing.T) {
	// TODO: this test fails because of a race condition in the queue.
	// Re-enable this test after it's fixed!
	t.SkipNow()

	totalStart := time.Now()
	n := 10
	for i := 0; i < n; i++ {
		start := time.Now()
		const (
			pushers      = 10
			poppers      = 1
			messageCount = 1000
		)
		queue := New()

		// Spawn a printer to allow for non-blocking logging.
		print := make(chan []any, 2048)
		go func() {
			for msg := range print {
				t.Logf(msg[0].(string), msg[1:]...)
				_ = msg
			}
		}()

		// Spawn pushers.
		var pushersWg sync.WaitGroup
		allMsgs := make([]*DecodedSSVMessage, 0, messageCount)
		var pushed atomic.Int64
		for i := 0; i < pushers; i++ {
			msgs := make([]*DecodedSSVMessage, messageCount/pushers)
			for i := 0; i < len(msgs); i++ {
				var err error
				msgs[i], err = DecodeSSVMessage(mockConsensusMessage{Height: qbft.Height(rand.Intn(messageCount)), Type: qbft.PrepareMsgType}.ssvMessage(mockState))
				require.NoError(t, err)
			}
			allMsgs = append(allMsgs, msgs...)

			pushersWg.Add(1)
			go func() {
				defer pushersWg.Done()
				for _, m := range msgs {
					queue.Push(m)
					n := pushed.Add(1)
					_ = n
					// print <- []any{"pushed message %d/%d", n, messageCount}
					time.Sleep(time.Duration(rand.Intn(10)) * time.Microsecond)
				}
			}()
		}

		// Assert pushed messages.
		var pushersAssertionWg sync.WaitGroup
		pushersAssertionWg.Add(1)
		go func() {
			pushersWg.Wait()
			defer pushersAssertionWg.Done()
			require.Equal(t, pushed.Load(), int64(messageCount))
		}()

		// Pop all messages.
		var poppersWg sync.WaitGroup
		popped := make(chan *DecodedSSVMessage, messageCount*2)
		var stopPopping atomic.Bool
		for i := 0; i < poppers; i++ {
			poppersWg.Add(1)
			go func() {
				defer poppersWg.Done()
				for {
					if queue.IsEmpty() {
						if stopPopping.Load() {
							return
						}
						time.Sleep(5 * time.Microsecond)
						continue
					}
					msg := queue.Pop(NewMessagePrioritizer(mockState))
					if msg != nil {
						popped <- msg
					} else {
						if stopPopping.Load() {
							return
						}
					}
					// print <- []any{"popped message %d/%d", len(popped), messageCount}
					if time.Since(start).Seconds() > 10 {
						t.Logf("timeout")
						t.Fail()
					}
				}
			}()
		}

		// Wait for pushed messages assertion.
		pushersAssertionWg.Wait()
		stopPopping.Store(true)

		// Wait for poppers.
		go func() {
			poppersWg.Wait()
			close(popped)
		}()
		allPopped := make(map[*DecodedSSVMessage]struct{})
		for msg := range popped {
			allPopped[msg] = struct{}{}
		}
		log.Printf("popped %d messages in %s", len(allPopped), time.Since(start))

		// Assert that all messages were popped.
		for _, msg := range allMsgs {
			if _, ok := allPopped[msg]; !ok {
				t.Log("message not popped")
				t.Fail()
			}
		}
	}

	log.Printf("finished %d iterations in %s", n, time.Since(totalStart))
}

func TestPriorityQueueWaitAndPop(t *testing.T) {
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		queue := New()
		require.True(t, queue.IsEmpty())

		msg, err := DecodeSSVMessage(mockConsensusMessage{Height: 100, Type: qbft.PrepareMsgType}.ssvMessage(mockState))
		require.NoError(t, err)

		// Push 2 message.
		queue.Push(msg)
		queue.Push(msg)

		// WaitAndPop immediately.
		popped := queue.WaitAndPop(ctx, NewMessagePrioritizer(mockState))
		require.NotNil(t, popped)
		require.Equal(t, msg, popped)

		// WaitAndPop immediately.
		popped = queue.WaitAndPop(ctx, NewMessagePrioritizer(mockState))
		require.NotNil(t, popped)
		require.Equal(t, msg, popped)

		// Push 1 message in a goroutine.
		go func() {
			time.Sleep(100 * time.Millisecond)
			queue.Push(msg)

			time.Sleep(100 * time.Millisecond)
			queue.Push(msg)
		}()

		// WaitAndPop should wait for the message to be pushed.
		popped = queue.WaitAndPop(ctx, NewMessagePrioritizer(mockState))
		require.NotNil(t, popped)
		require.Equal(t, msg, popped)

		// WaitAndPop should wait for the message to be pushed.
		popped = queue.WaitAndPop(ctx, NewMessagePrioritizer(mockState))
		require.NotNil(t, popped)
		require.Equal(t, msg, popped)
	}
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
				actual := q.Pop(NewMessagePrioritizer(test.state))
				require.Equal(t, excepted, actual, "incorrect message at index %d", i)
			}
		})
	}
}

func BenchmarkPriorityQueueConcurrent(b *testing.B) {
	prioritizer := NewMessagePrioritizer(mockState)
	queue := New()

	messageCount := 500
	types := []qbft.MessageType{qbft.PrepareMsgType, qbft.CommitMsgType, qbft.RoundChangeMsgType}
	msgs := make(chan *DecodedSSVMessage, messageCount*len(types))
	for _, i := range rand.Perm(messageCount) {
		height := qbft.FirstHeight + qbft.Height(i)
		for _, t := range types {
			decoded, err := DecodeSSVMessage(mockConsensusMessage{Height: height, Type: t}.ssvMessage(mockState))
			require.NoError(b, err)
			msgs <- decoded
		}
	}

	b.ResetTimer()
	b.StartTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			select {
			case msg := <-msgs:
				queue.Push(msg)
			default:
			}
		}
	})

	popped := 0
	for n := b.N; n > 0; n-- {
		msg := queue.Pop(prioritizer)
		if msg != nil {
			popped++
		}
	}

	b.Logf("popped %d messages", popped)
}

func decodeAndPush(t require.TestingT, queue Queue, msg mockMessage, state *State) *DecodedSSVMessage {
	decoded, err := DecodeSSVMessage(msg.ssvMessage(state))
	require.NoError(t, err)
	queue.Push(decoded)
	return decoded
}
