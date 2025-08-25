package worker

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/network"
	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
)

func TestWorker(t *testing.T) {
	logger := log.TestLogger(t)
	worker := NewWorker(logger, &Config{
		Ctx:          t.Context(),
		WorkersCount: 1,
		Buffer:       2,
	})

	worker.UseHandler(func(ctx context.Context, msg network.DecodedSSVMessage) error {
		require.NotNil(t, msg)
		return nil
	})
	for i := 0; i < 5; i++ {
		require.True(t, worker.TryEnqueue(&queue.SSVMessage{}))
		time.Sleep(time.Second * 1)
	}
}

func TestManyWorkers(t *testing.T) {
	logger := log.TestLogger(t)
	var wg sync.WaitGroup

	worker := NewWorker(logger, &Config{
		Ctx:          t.Context(),
		WorkersCount: 10,
		Buffer:       0,
	})
	time.Sleep(time.Millisecond * 100) // wait for worker to start listen

	worker.UseHandler(func(ctx context.Context, msg network.DecodedSSVMessage) error {
		require.NotNil(t, msg)
		wg.Done()
		return nil
	})

	for i := 0; i < 10; i++ {
		wg.Add(1)
		require.True(t, worker.TryEnqueue(&queue.SSVMessage{}))
	}
	wg.Wait()
}

func TestBuffer(t *testing.T) {
	logger := log.TestLogger(t)
	var wg sync.WaitGroup

	worker := NewWorker(logger, &Config{
		Ctx:          t.Context(),
		WorkersCount: 1,
		Buffer:       10,
	})
	time.Sleep(time.Millisecond * 100) // wait for worker to start listen

	worker.UseHandler(func(ctx context.Context, msg network.DecodedSSVMessage) error {
		require.NotNil(t, msg)
		wg.Done()
		time.Sleep(time.Millisecond * 100)
		return nil
	})

	for i := 0; i < 11; i++ { // should buffer 10 msgs
		wg.Add(1)
		require.True(t, worker.TryEnqueue(&queue.SSVMessage{}))
	}
	wg.Wait()
}
