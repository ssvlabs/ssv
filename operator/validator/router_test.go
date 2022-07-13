package validator

import (
	"context"
	"fmt"
	forksv2 "github.com/bloxapp/ssv/network/forks/v2"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"sync"
	"testing"
)

func TestRouter(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	router := newMessageRouter(zap.L(), forksv2.New().MsgID())

	expectedCount := 1000
	count := 0

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()

		cn := router.GetMessageChan()
		for msg := range cn {
			require.NotNil(t, msg)
			count++
			if ctx.Err() != nil || count >= expectedCount {
				return
			}
		}
	}()

	for i := 0; i < expectedCount; i++ {
		msg := message.SSVMessage{
			MsgType: message.MsgType(i % 3),
			ID:      message.NewIdentifier([]byte{1, 1, 1, 1, 1}, message.RoleTypeAttester),
			Data:    []byte(fmt.Sprintf("data-%d", i)),
		}
		router.Route(msg)
		if i%2 == 0 {
			go router.Route(msg)
		}
	}

	wg.Wait()

	require.Equal(t, count, expectedCount)
}
