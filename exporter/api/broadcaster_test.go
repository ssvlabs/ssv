package api

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/prysmaticlabs/prysm/v4/async/event"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// TestConn_Send_FullQueue asserts the broadcasted.Send contract for *conn:
// Send must not block, even when called past the queue capacity, and on
// overflow it cancels the conn ctx (the teardown signal consumed by
// WriteLoop / ReadLoop / handleStream defers).
func TestConn_Send_FullQueue(t *testing.T) {
	c := newConn(t.Context(), nil, "test", 0, false).(*conn)
	require.NoError(t, c.ctx.Err(), "ctx should not be canceled before overflow")

	for i := 0; i < chanSize+2; i++ {
		c.Send([]byte(fmt.Sprintf("test-%d", i)))
	}
	require.Error(t, c.ctx.Err(), "ctx should be canceled after overflow")
}

// TestBroadcaster verifies the basic register / fan-out / deregister flow:
// feed.Send delivers to every registered connection, a Deregister'd
// connection stops receiving, and Register/Deregister are idempotent
// against duplicate calls. No timing assumptions: each step waits for
// delivery via Eventually before issuing the next operation.
func TestBroadcaster(t *testing.T) {
	logger := zaptest.NewLogger(t)
	b := newBroadcaster(logger)

	feed := new(event.Feed)
	b.FromFeed(t.Context(), feed)
	bm1 := newBroadcastedMock("1")
	bm2 := newBroadcastedMock("2")

	require.True(t, b.Register(bm1))
	require.False(t, b.Register(bm1), "Register must be idempotent for the same id")
	defer b.Deregister(bm1)
	require.True(t, b.Register(bm2))

	// First message reaches both connections.
	feed.Send(Message{Type: TypeValidator, Filter: MessageFilter{From: 0, To: 0}})
	require.Eventually(t, func() bool {
		return bm1.Size() == 1 && bm2.Size() == 1
	}, 2*time.Second, 5*time.Millisecond)

	// Deregister bm2 — second call returns false (idempotent).
	require.True(t, b.Deregister(bm2))
	require.False(t, b.Deregister(bm2))

	// Second message reaches only bm1.
	feed.Send(Message{Type: TypeValidator, Filter: MessageFilter{From: 0, To: 0}})
	require.Eventually(t, func() bool { return bm1.Size() == 2 }, 2*time.Second, 5*time.Millisecond)
	require.Equal(t, 1, bm2.Size(), "bm2 must not receive after deregister")
}

type broadcastedMock struct {
	mut  sync.Mutex
	msgs [][]byte
	id   string
}

func newBroadcastedMock(id string) *broadcastedMock {
	return &broadcastedMock{
		msgs: [][]byte{},
		id:   id,
	}
}

func (b *broadcastedMock) ID() string {
	return b.id
}

func (b *broadcastedMock) Send(msg []byte) {
	b.mut.Lock()
	defer b.mut.Unlock()
	b.msgs = append(b.msgs, msg)
}

func (b *broadcastedMock) Size() int {
	b.mut.Lock()
	defer b.mut.Unlock()

	return len(b.msgs)
}
