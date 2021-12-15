package listeners

import (
	"context"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestListenersContainer(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lsCont := NewListenersContainer(ctx, logger)

	var wg sync.WaitGroup

	n := 100

	// counting incoming IBFT messages (should get n messages)
	var count int32
	l := NewListener(network.NetworkMsg_IBFTType)
	deregister := lsCont.Register(l)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer deregister()

		cn := l.MsgChan()
		for msg := range cn {
			require.NotNil(t, msg)
			atomic.AddInt32(&count, 1)
			if atomic.LoadInt32(&count) == int32(n-1) {
				return
			}
		}
	}()

	propagate := func(msg *proto.SignedMessage) {
		lss := lsCont.GetListeners(network.NetworkMsg_IBFTType)
		for _, ls := range lss {
			ls.MsgChan() <- msg
		}
	}

	// sending n messages
	go func() {
		for i := 0; i < n; i++ {
			go propagate(&proto.SignedMessage{
				Message: &proto.Message{
					Type:      proto.RoundState_ChangeRound,
					Round:     1,
					Lambda:    []byte{1, 2, 3, 4},
					SeqNumber: uint64(i + 1),
					Value:     []byte{},
				},
				Signature: []byte{},
				SignerIds: []uint64{1},
			})
		}
	}()

	wg.Wait()

	require.Equal(t, int32(n-1), atomic.LoadInt32(&count))
	// wait for de-registration and ensure listener is not available afterwards
	time.After(time.Millisecond * 100)
	lss := lsCont.GetListeners(network.NetworkMsg_IBFTType)
	require.Equal(t, 0, len(lss))
}
