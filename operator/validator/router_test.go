package validator

import (
	"context"
	"fmt"
	"sync"
	"testing"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/network/forks/genesis"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/stretchr/testify/require"
)

func TestRouter(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := logex.TestLogger(t)

	router := newMessageRouter(genesis.New().MsgID())

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
		router.Route(logger, msg)
		if i%2 == 0 {
			go router.Route(logger, msg)
		}
	}

	wg.Wait()

	require.Equal(t, count, expectedCount)
}
