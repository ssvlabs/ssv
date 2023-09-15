package queue

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

var mockState = &State{
	HasRunningInstance: true,
	Height:             100,
	Slot:               64,
	Quorum:             4,
}

func TestPriorityQueue_TryPop(t *testing.T) {
	queue := NewDefault()
	require.True(t, queue.Empty())

	// Push 2 messages.
	msg1 := decodeAndPush(t, queue, mockConsensusMessage{Height: 100, Type: qbft.PrepareMsgType}, mockState)
	msg2 := decodeAndPush(t, queue, mockConsensusMessage{Height: 101, Type: qbft.PrepareMsgType}, mockState)
	require.False(t, queue.Empty())

	// Pop 1st message.
	popped := queue.TryPop(NewMessagePrioritizer(mockState), FilterAny)
	require.Equal(t, msg1, popped)

	// Pop 2nd message.
	popped = queue.TryPop(NewMessagePrioritizer(mockState), FilterAny)
	require.True(t, queue.Empty())
	require.NotNil(t, popped)
	require.Equal(t, msg2, popped)

	// Pop nil.
	popped = queue.TryPop(NewMessagePrioritizer(mockState), FilterAny)
	require.Nil(t, popped)
}

func TestPriorityQueue_Filter(t *testing.T) {
	queue := NewDefault()
	require.True(t, queue.Empty())

	// Push 1 message.
	msg := decodeAndPush(t, queue, mockConsensusMessage{Height: 100, Type: qbft.PrepareMsgType}, mockState)
	require.False(t, queue.Empty())

	// Pop non-matching message.
	popped := queue.TryPop(NewMessagePrioritizer(mockState), func(msg *DecodedSSVMessage) bool {
		return msg.Body.(*qbft.SignedMessage).Message.Height == 101
	})
	require.False(t, queue.Empty())
	require.Nil(t, popped)

	// Pop matching message.
	popped = queue.TryPop(NewMessagePrioritizer(mockState), func(msg *DecodedSSVMessage) bool {
		return msg.Body.(*qbft.SignedMessage).Message.Height == 100
	})
	require.True(t, queue.Empty())
	require.NotNil(t, popped)
	require.Equal(t, msg, popped)

	// Push 2 messages.
	msg1 := decodeAndPush(t, queue, mockConsensusMessage{Height: 100, Type: qbft.PrepareMsgType}, mockState)
	msg2 := decodeAndPush(t, queue, mockConsensusMessage{Height: 101, Type: qbft.PrepareMsgType}, mockState)

	// Pop 2nd message.
	popped = queue.TryPop(NewMessagePrioritizer(mockState), func(msg *DecodedSSVMessage) bool {
		return msg.Body.(*qbft.SignedMessage).Message.Height == 101
	})
	require.NotNil(t, popped)
	require.Equal(t, msg2, popped)

	// Pop 1st message.
	popped = queue.TryPop(NewMessagePrioritizer(mockState), func(msg *DecodedSSVMessage) bool {
		return msg.Body.(*qbft.SignedMessage).Message.Height == 100
	})
	require.True(t, queue.Empty())
	require.NotNil(t, popped)
	require.Equal(t, msg1, popped)

	// Pop nil.
	popped = queue.TryPop(NewMessagePrioritizer(mockState), func(msg *DecodedSSVMessage) bool {
		return msg.Body.(*qbft.SignedMessage).Message.Height == 100
	})
	require.Nil(t, popped)
}

func TestPriorityQueue_Pop(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	const (
		capacity       = 3
		contextTimeout = 50 * time.Millisecond
		pushDelay      = 50 * time.Millisecond
		precision      = 5 * time.Millisecond
	)
	queue := New(capacity)
	require.True(t, queue.Empty())

	msg, err := DecodeSSVMessage(mockConsensusMessage{Height: 100, Type: qbft.PrepareMsgType}.ssvMessage(mockState))
	require.NoError(t, err)

	// Push messages.
	for i := 0; i < capacity; i++ {
		queue.TryPush(msg)
	}
	require.False(t, queue.Empty())

	// Should pop everything immediately.
	for i := 0; i < capacity; i++ {
		popped := queue.Pop(ctx, NewMessagePrioritizer(mockState), FilterAny)
		require.Equal(t, msg, popped)
	}
	require.True(t, queue.Empty())

	// Should pop nil after context cancellation.
	{
		ctx, cancel := context.WithTimeout(context.Background(), contextTimeout)
		defer cancel()
		expectedEnd := time.Now().Add(contextTimeout)
		popped := queue.Pop(ctx, NewMessagePrioritizer(mockState), FilterAny)
		require.Nil(t, popped)
		require.WithinDuration(t, expectedEnd, time.Now(), precision)
	}

	// Push messages in a goroutine.
	go func() {
		for i := 0; i < capacity; i++ {
			time.Sleep(pushDelay)
			queue.TryPush(msg)
		}
	}()

	// Should wait for the messages to be pushed.
	expectedEnd := time.Now().Add(pushDelay * time.Duration(capacity))
	for i := 0; i < capacity; i++ {
		popped := queue.Pop(ctx, NewMessagePrioritizer(mockState), FilterAny)
		require.Equal(t, msg, popped)
	}
	require.True(t, queue.Empty())
	require.WithinDuration(t, expectedEnd, time.Now(), precision)
}

// TestPriorityQueue_Order tests that the queue returns the messages in the correct order.
func TestPriorityQueue_Order(t *testing.T) {
	for _, test := range messagePriorityTests {
		t.Run(fmt.Sprintf("PriorityQueue: %s", test.name), func(t *testing.T) {
			// Create the PriorityQueue and populate it with messages.
			q := NewDefault()

			// Decode messages.
			messages := make(messageSlice, len(test.messages))
			for i, m := range test.messages {
				mm, err := DecodeSSVMessage(m.ssvMessage(test.state))
				require.NoError(t, err)
				messages[i] = mm
			}

			for i := 0; i < 20; i++ {
				// Push shuffled messages to the queue.
				for _, msg := range messages.shuffle() {
					q.Push(msg)
				}

				// Pop messages from the queue and compare to the expected order.
				for i, excepted := range messages {
					actual := q.TryPop(NewMessagePrioritizer(test.state), FilterAny)
					require.Equal(t, excepted, actual, "incorrect message at index %d", i)
				}
			}
		})
	}
}

type testMetrics struct {
	dropped atomic.Uint64
}

func (n *testMetrics) DroppedQueueMessage(messageID spectypes.MessageID) {
	n.dropped.Add(1)
}

func TestWithMetrics(t *testing.T) {
	metrics := &testMetrics{}
	queue := WithMetrics(New(1), metrics)
	require.True(t, queue.Empty())

	// Push 1 message.
	msg, err := DecodeSSVMessage(mockConsensusMessage{Height: 100, Type: qbft.PrepareMsgType}.ssvMessage(mockState))
	require.NoError(t, err)
	pushed := queue.TryPush(msg)
	require.True(t, pushed)
	require.False(t, queue.Empty())
	require.EqualValues(t, 0, metrics.dropped.Load())

	// Push above capacity.
	pushed = queue.TryPush(msg)
	require.False(t, pushed)
	require.False(t, queue.Empty())
	require.EqualValues(t, 1, metrics.dropped.Load())
}

func BenchmarkPriorityQueue_Parallel(b *testing.B) {
	benchmarkPriorityQueueParallel(b, func() Queue {
		return New(32)
	}, false)
}

func BenchmarkPriorityQueue_Parallel_Lossy(b *testing.B) {
	benchmarkPriorityQueueParallel(b, NewDefault, true)
}

func benchmarkPriorityQueueParallel(b *testing.B, factory func() Queue, lossy bool) {
	english := message.NewPrinter(language.English)

	const (
		pushers      = 16
		poppers      = 1
		messageCount = 2080
	)
	queue := factory()

	// Prepare messages.
	messages := make([]*DecodedSSVMessage, messageCount)
	for i := range messages {
		var err error
		msg, err := DecodeSSVMessage(mockConsensusMessage{Height: qbft.Height(rand.Intn(messageCount)), Type: qbft.PrepareMsgType}.ssvMessage(mockState))
		require.NoError(b, err)
		messages[i] = msg
	}

	// Metrics.
	var (
		totalPushed   int
		totalPopped   int
		totalDuration time.Duration
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()

		// Stream messages to pushers.
		messageStream := make(chan *DecodedSSVMessage)
		go func() {
			for _, msg := range messages {
				messageStream <- msg
			}
			close(messageStream)
		}()

		// Spawn pushers.
		var (
			pushersWg   sync.WaitGroup
			pushedCount atomic.Int64
		)
		for i := 0; i < pushers; i++ {
			pushersWg.Add(1)
			go func() {
				defer pushersWg.Done()
				for m := range messageStream {
					if lossy {
						queue.TryPush(m)
					} else {
						queue.Push(m)
					}
					pushedCount.Add(1)
					time.Sleep(time.Duration(rand.Intn(300)) * time.Microsecond)
				}
			}()
		}

		// Assert pushed messages.
		var pushersAssertionWg sync.WaitGroup
		pushersAssertionWg.Add(1)
		go func() {
			pushersWg.Wait()
			defer pushersAssertionWg.Done()
			totalPushed += messageCount
			require.Equal(b, int64(messageCount), pushedCount.Load())
		}()

		// Pop all messages.
		var poppersWg sync.WaitGroup
		popped := make(chan *DecodedSSVMessage, messageCount*2)
		poppingCtx, stopPopping := context.WithCancel(context.Background())
		for i := 0; i < poppers; i++ {
			poppersWg.Add(1)
			go func() {
				defer poppersWg.Done()
				for {
					msg := queue.Pop(poppingCtx, NewMessagePrioritizer(mockState), FilterAny)
					if msg == nil {
						return
					}
					popped <- msg
				}
			}()
		}

		// Wait for pushed messages assertion.
		pushersAssertionWg.Wait()
		stopPopping()

		// Wait for poppers.
		go func() {
			poppersWg.Wait()
			close(popped)
		}()

		allPopped := make(map[*DecodedSSVMessage]struct{})
		for msg := range popped {
			allPopped[msg] = struct{}{}
		}
		totalPopped += len(allPopped)
		totalDuration += time.Since(start)

		if !lossy {
			// Assert that all messages were popped.
			require.Equal(b, messageCount, len(allPopped), "popped messages count mismatch")

			for _, msg := range messages {
				if _, ok := allPopped[msg]; !ok {
					b.Log("message not popped")
					b.Fail()
				}
			}
		}
	}
	b.StopTimer()

	// Log metrics.
	b.Log(english.Sprintf(
		"popped %d/%d (%.3f%% loss) messages at %d messages/sec",
		totalPopped,
		totalPushed,
		100*(1-float64(totalPopped)/float64(totalPushed)),
		int64(float64(totalPopped)/totalDuration.Seconds()),
	))
}

func BenchmarkPriorityQueue_Concurrent(b *testing.B) {
	prioritizer := NewMessagePrioritizer(mockState)
	queue := NewDefault()

	messageCount := 10_000
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

	var pushersWg sync.WaitGroup
	var pushed atomic.Int32
	for i := 0; i < 16; i++ {
		pushersWg.Add(1)
		go func() {
			defer pushersWg.Done()
			for n := b.N; n > 0; n-- {
				select {
				case msg := <-msgs:
					queue.Push(msg)
					pushed.Add(1)
				default:
				}
			}
		}()
	}

	pushersCtx, cancel := context.WithCancel(context.Background())
	go func() {
		pushersWg.Wait()
		cancel()
	}()

	var popperWg sync.WaitGroup
	popperWg.Add(1)
	popped := 0
	go func() {
		defer popperWg.Done()
		for n := b.N; n > 0; n-- {
			msg := queue.Pop(pushersCtx, prioritizer, FilterAny)
			if msg == nil {
				return
			}
			popped++
		}
	}()

	popperWg.Wait()

	b.Logf("popped %d messages", popped)
	b.Logf("pushed %d messages", pushed.Load())
}

func decodeAndPush(t require.TestingT, queue Queue, msg mockMessage, state *State) *DecodedSSVMessage {
	decoded, err := DecodeSSVMessage(msg.ssvMessage(state))
	require.NoError(t, err)
	queue.Push(decoded)
	return decoded
}
