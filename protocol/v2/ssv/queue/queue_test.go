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

func TestPriorityQueuePushAndPop(t *testing.T) {
	queue := NewDefault()

	require.True(t, queue.Empty())

	// Push 2 messages.
	msg := decodeAndPush(t, queue, mockConsensusMessage{Height: 100, Type: qbft.PrepareMsgType}, mockState)
	msg2 := decodeAndPush(t, queue, mockConsensusMessage{Height: 101, Type: qbft.PrepareMsgType}, mockState)
	require.False(t, queue.Empty())

	// Pop 1st message.
	popped := queue.TryPop(NewMessagePrioritizer(mockState))
	require.Equal(t, msg, popped)

	// Pop 2nd message.
	popped = queue.TryPop(NewMessagePrioritizer(mockState))
	require.True(t, queue.Empty())
	require.NotNil(t, popped)
	require.Equal(t, msg2, popped)

	// Pop nil.
	popped = queue.TryPop(NewMessagePrioritizer(mockState))
	require.Nil(t, popped)
}

func BenchmarkPriorityQueue_Parallel(b *testing.B) {
	benchmarkPriorityQueueParallel(b, func() Queue {
		return New(32, PusherBlocking())
	}, false)
}

func BenchmarkPriorityQueue_Parallel_Lossy(b *testing.B) {
	benchmarkPriorityQueueParallel(b, NewDefault, true)
}

func BenchmarkPriorityQueue_Parallel_AtomicPointer_Lossy(b *testing.B) {
	benchmarkPriorityQueueParallel(b, NewAtomicPointer, true)
}

func benchmarkPriorityQueueParallel(b *testing.B, factory func() Queue, lossy bool) {
	english := message.NewPrinter(language.English)

	const (
		pushers      = 16
		poppers      = 1
		messageCount = 2080
	)
	queue := factory()

	// Spawn a printer to allow for non-blocking logging.
	print := make(chan []any, 2048)
	go func() {
		for msg := range print {
			b.Logf(msg[0].(string), msg[1:]...)
			_ = msg
		}
	}()

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
					queue.Push(m)
					pushedCount.Add(1)
					// print <- []any{"pushed message %d/%d", n, messageCount}
					time.Sleep(time.Duration(rand.Intn(5)) * time.Microsecond)
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
					msg := queue.Pop(poppingCtx, NewMessagePrioritizer(mockState))
					if msg == nil {
						return
					}
					if msg == nil {
						b.Logf("nil message")
						b.Fail()
					}
					popped <- msg
					// print <- []any{"popped message %d/%d", len(popped), messageCount}
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
		"popped %d/%d (%.f%% loss) messages at %d messages/sec",
		totalPopped,
		totalPushed,
		100*(1-float64(totalPopped)/float64(totalPushed)),
		int64(float64(totalPopped)/totalDuration.Seconds()),
	))
}

func TestPriorityQueue_Pop(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for i := 0; i < 10; i++ {
		queue := NewDefault()
		require.True(t, queue.Empty())

		msg, err := DecodeSSVMessage(mockConsensusMessage{Height: 100, Type: qbft.PrepareMsgType}.ssvMessage(mockState))
		require.NoError(t, err)

		// Push 2 message.
		queue.Push(msg)
		queue.Push(msg)

		// Pop immediately.
		popped := queue.Pop(ctx, NewMessagePrioritizer(mockState))
		require.NotNil(t, popped)
		require.Equal(t, msg, popped)

		// Pop immediately.
		popped = queue.Pop(ctx, NewMessagePrioritizer(mockState))
		require.NotNil(t, popped)
		require.Equal(t, msg, popped)

		// Push 1 message in a goroutine.
		go func() {
			time.Sleep(100 * time.Millisecond)
			queue.Push(msg)

			time.Sleep(100 * time.Millisecond)
			queue.Push(msg)
		}()

		// Pop should wait for the message to be pushed.
		popped = queue.Pop(ctx, NewMessagePrioritizer(mockState))
		require.NotNil(t, popped)
		require.Equal(t, msg, popped)

		// Pop should wait for the message to be pushed.
		popped = queue.Pop(ctx, NewMessagePrioritizer(mockState))
		require.NotNil(t, popped)
		require.Equal(t, msg, popped)
	}
}

// TestPriorityQueue_Order tests that the queue returns the messages in the correct order.
func TestPriorityQueue_Order(t *testing.T) {
	for _, test := range messagePriorityTests {
		t.Run(fmt.Sprintf("PriorityQueue: %s", test.name), func(t *testing.T) {
			// Create the PriorityQueue and populate it with messages.
			q := NewDefault()

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
				actual := q.TryPop(NewMessagePrioritizer(test.state))
				require.Equal(t, excepted, actual, "incorrect message at index %d", i)
			}
		})
	}
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
			msg := queue.Pop(pushersCtx, prioritizer)
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
