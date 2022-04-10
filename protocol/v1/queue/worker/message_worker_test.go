package worker

import (
	"context"
	"testing"
	"time"

	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/stretchr/testify/require"
)

func TestWorker(t *testing.T) {
	worker := NewWorker(&Config{
		Ctx:          context.Background(),
		WorkersCount: 1,
		Buffer:       2,
	})

	worker.AddHandler(func(msg *message.SSVMessage) {
		require.NotNil(t, msg)
	})
	for i := 0; i < 5; i++ {
		require.True(t, worker.TryEnqueue(&message.SSVMessage{}))
		time.Sleep(time.Second * 1)
	}
}
