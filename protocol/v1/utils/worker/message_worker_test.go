package worker

import (
	"context"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestWorker(t *testing.T) {
	worker := NewWorker(&WorkerConfig{
		Ctx:          context.Background(),
		WorkersCount: 1,
		Buffer:       2,
	})

	worker.AddHandler(func(msg *message.SignedMessage) {
		require.NotNil(t, msg)
	})
	worker.Init()
	for i := 0; i < 5; i++ {
		require.True(t, worker.TryEnqueue(&message.SignedMessage{}))
		time.Sleep(time.Second * 1)
	}
}
