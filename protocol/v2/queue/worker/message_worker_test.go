package worker

import (
	"context"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"go.uber.org/zap"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap/zapcore"

	"github.com/bloxapp/ssv/utils/logex"

	"github.com/stretchr/testify/require"
)

func init() {
	logex.Build("test", zapcore.DebugLevel, nil)
}

func TestWorker(t *testing.T) {
	logger := logex.GetLogger()
	worker := NewWorker(logger, &Config{
		Ctx:          context.Background(),
		WorkersCount: 1,
		Buffer:       2,
	})

	worker.UseHandler(func(logger *zap.Logger, msg *spectypes.SSVMessage) error {
		require.NotNil(t, msg)
		return nil
	})
	for i := 0; i < 5; i++ {
		require.True(t, worker.TryEnqueue(&spectypes.SSVMessage{}))
		time.Sleep(time.Second * 1)
	}
}

func TestManyWorkers(t *testing.T) {
	logger := logex.GetLogger()
	var wg sync.WaitGroup

	worker := NewWorker(logger, &Config{
		Ctx:          context.Background(),
		WorkersCount: 10,
		Buffer:       0,
	})
	time.Sleep(time.Millisecond * 100) // wait for worker to start listen

	worker.UseHandler(func(logger *zap.Logger, msg *spectypes.SSVMessage) error {
		require.NotNil(t, msg)
		wg.Done()
		return nil
	})

	for i := 0; i < 10; i++ {
		wg.Add(1)
		require.True(t, worker.TryEnqueue(&spectypes.SSVMessage{}))
	}
	wg.Wait()
}

func TestBuffer(t *testing.T) {
	logger := logex.GetLogger()
	var wg sync.WaitGroup

	worker := NewWorker(logger, &Config{
		Ctx:          context.Background(),
		WorkersCount: 1,
		Buffer:       10,
	})
	time.Sleep(time.Millisecond * 100) // wait for worker to start listen

	worker.UseHandler(func(logger *zap.Logger, msg *spectypes.SSVMessage) error {
		require.NotNil(t, msg)
		wg.Done()
		time.Sleep(time.Millisecond * 100)
		return nil
	})

	for i := 0; i < 11; i++ { // should buffer 10 msgs
		wg.Add(1)
		require.True(t, worker.TryEnqueue(&spectypes.SSVMessage{}))
	}
	wg.Wait()
}
