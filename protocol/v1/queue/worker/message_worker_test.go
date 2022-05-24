package worker

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/message"
)

func TestWorker(t *testing.T) {
	worker := NewWorker(&Config{
		Ctx:          context.Background(),
		Logger:       zap.L(),
		WorkersCount: 1,
		Buffer:       2,
	})

	worker.AddHandler(func(msg *message.SSVMessage) error {
		require.NotNil(t, msg)
		return nil
	})
	for i := 0; i < 5; i++ {
		require.True(t, worker.TryEnqueue(&message.SSVMessage{}))
		time.Sleep(time.Second * 1)
	}
}
