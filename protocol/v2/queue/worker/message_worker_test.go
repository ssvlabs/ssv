package worker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/network"
	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
)

func TestWorker(t *testing.T) {
	testCtx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	logger := log.TestLogger(t)
	worker := NewWorker(logger, &Config{
		Ctx:          testCtx,
		WorkersCount: 1,
		Buffer:       2,
	})

	handlerErrCh := make(chan error, 1)
	processed := make(chan struct{}, 1)
	worker.UseHandler(func(ctx context.Context, msg network.DecodedSSVMessage) error {
		if msg == nil {
			select {
			case handlerErrCh <- errors.New("received nil message"):
			default:
			}
			return nil
		}
		processed <- struct{}{}
		return nil
	})

	for i := 0; i < 5; i++ {
		require.True(t, worker.TryEnqueue(&queue.SSVMessage{}))
		select {
		case <-processed:
		case <-testCtx.Done():
			t.Fatalf("timed out waiting for message %d to be processed", i)
		}
	}
	assertNoAsyncError(t, handlerErrCh)
}

func TestManyWorkers(t *testing.T) {
	testCtx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	logger := log.TestLogger(t)
	var wg sync.WaitGroup

	worker := NewWorker(logger, &Config{
		Ctx:          testCtx,
		WorkersCount: 10,
		Buffer:       10,
	})

	handlerErrCh := make(chan error, 1)
	worker.UseHandler(func(ctx context.Context, msg network.DecodedSSVMessage) error {
		if msg == nil {
			select {
			case handlerErrCh <- errors.New("received nil message"):
			default:
			}
			return nil
		}
		wg.Done()
		return nil
	})

	for i := 0; i < 10; i++ {
		wg.Add(1)
		require.True(t, worker.TryEnqueue(&queue.SSVMessage{}))
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		wg.Wait()
	}()

	select {
	case <-done:
	case <-testCtx.Done():
		t.Fatal("timed out waiting for workers to process messages")
	}
	assertNoAsyncError(t, handlerErrCh)
}

func TestBuffer(t *testing.T) {
	testCtx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	logger := log.TestLogger(t)
	var wg sync.WaitGroup

	worker := NewWorker(logger, &Config{
		Ctx:          testCtx,
		WorkersCount: 1,
		Buffer:       10,
	})

	const totalMessages = 11
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	wg.Add(totalMessages)
	handlerErrCh := make(chan error, 1)

	worker.UseHandler(func(ctx context.Context, msg network.DecodedSSVMessage) error {
		if msg == nil {
			select {
			case handlerErrCh <- errors.New("received nil message"):
			default:
			}
			return nil
		}
		once.Do(func() {
			close(started)
		})
		<-release
		wg.Done()
		return nil
	})

	// Let one message start processing, then fill the queue buffer.
	require.True(t, worker.TryEnqueue(&queue.SSVMessage{}))
	select {
	case <-started:
	case <-testCtx.Done():
		t.Fatal("timed out waiting for handler to start")
	}

	for i := 0; i < totalMessages-1; i++ { // should fill the 10-sized buffer
		require.True(t, worker.TryEnqueue(&queue.SSVMessage{}))
	}
	require.False(t, worker.TryEnqueue(&queue.SSVMessage{}), "queue should be full")

	close(release)

	done := make(chan struct{})
	go func() {
		defer close(done)
		wg.Wait()
	}()

	select {
	case <-done:
	case <-testCtx.Done():
		t.Fatal("timed out waiting for buffered messages to be processed")
	}
	assertNoAsyncError(t, handlerErrCh)
}

func assertNoAsyncError(t *testing.T, errCh <-chan error) {
	t.Helper()
	select {
	case err := <-errCh:
		require.NoError(t, err)
	default:
	}
}
