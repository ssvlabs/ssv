package validator

import (
	"context"
	"fmt"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/network/forks/genesis"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"sync"
	"testing"
)

func TestRouter(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	router := newMessageRouter(zap.L(), genesis.New().MsgID())

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
		msg := spectypes.SSVMessage{
			MsgType: spectypes.MsgType(i % 3),
			MsgID:   spectypes.NewMsgID([]byte{1, 1, 1, 1, 1}, spectypes.BNRoleAttester),
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
