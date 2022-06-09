package worker

import (
	"context"
	"github.com/bloxapp/ssv/utils/logex"
	"go.uber.org/zap/zapcore"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/message"
)

func init() {
	logex.Build("test", zapcore.DebugLevel, nil)
}

func TestWorker(t *testing.T) {
	worker := NewWorker(&Config{
		Ctx:          context.Background(),
		Logger:       zap.L(),
		WorkersCount: 1,
		Buffer:       2,
	})

	worker.SetHandler(func(msg *message.SSVMessage) error {
		require.NotNil(t, msg)
		return nil
	})
	for i := 0; i < 5; i++ {
		require.True(t, worker.TryEnqueue(&message.SSVMessage{}))
		time.Sleep(time.Second * 1)
	}
}

func TestManyWorkers(t *testing.T) {
	var wg sync.WaitGroup

	worker := NewWorker(&Config{
		Ctx:          context.Background(),
		Logger:       zap.L(),
		WorkersCount: 10,
		Buffer:       0,
	})
	time.Sleep(time.Millisecond * 100) // wait for worker to start listen

	worker.SetHandler(func(msg *message.SSVMessage) error {
		require.NotNil(t, msg)
		wg.Done()
		return nil
	})

	for i := 0; i < 10; i++ {
		wg.Add(1)
		require.True(t, worker.TryEnqueue(&message.SSVMessage{}))
	}
	wg.Wait()
}

func TestBuffer(t *testing.T) {
	var wg sync.WaitGroup

	worker := NewWorker(&Config{
		Ctx:          context.Background(),
		Logger:       logex.GetLogger(),
		WorkersCount: 1,
		Buffer:       10,
	})
	time.Sleep(time.Millisecond * 100) // wait for worker to start listen

	worker.SetHandler(func(msg *message.SSVMessage) error {
		require.NotNil(t, msg)
		wg.Done()
		time.Sleep(time.Millisecond * 100)
		return nil
	})

	for i := 0; i < 11; i++ { // should buffer 10 msgs
		wg.Add(1)
		require.True(t, worker.TryEnqueue(&message.SSVMessage{}))
	}
	wg.Wait()
}
