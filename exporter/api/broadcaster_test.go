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

func TestConn_Send_FullQueue(t *testing.T) {
	c := newConn(t.Context(), nil, "test", 0, false)

	for i := 0; i < chanSize+2; i++ {
		c.Send([]byte(fmt.Sprintf("test-%d", i)))
	}
}

func TestBroadcaster(t *testing.T) {
	logger := zaptest.NewLogger(t)
	b := newBroadcaster(logger)

	feed := new(event.Feed)
	b.FromFeed(t.Context(), feed)
	bm1 := newBroadcastedMock("1")
	bm2 := newBroadcastedMock("2")

	require.True(t, b.Register(bm1))
	defer b.Deregister(bm1)

	require.True(t, b.Register(bm2))

	// Both subscribers are registered — both must receive msg1.
	feed.Send(Message{Type: TypeValidator, Filter: MessageFilter{From: 0, To: 0}})
	require.Eventually(t, func() bool {
		return bm1.Size() == 1 && bm2.Size() == 1
	}, time.Second, 5*time.Millisecond, "both subscribers should receive the first message")

	// Waiting above guarantees Broadcast(msg1) finished dispatching, so this
	// Deregister lands before any Broadcast snapshot for msg2 can see bm2.
	require.True(t, b.Deregister(bm2))

	// Only bm1 is registered — only bm1 must receive msg2.
	feed.Send(Message{Type: TypeValidator, Filter: MessageFilter{From: 0, To: 0}})
	require.Eventually(t, func() bool {
		return bm1.Size() == 2
	}, time.Second, 5*time.Millisecond, "bm1 should receive the second message")

	require.Equal(t, 1, bm2.Size(), "bm2 must not receive messages after deregister")
}

type broadcastedMock struct {
	mut  sync.Mutex
	msgs [][]byte
	id   string
}

func newBroadcastedMock(id string) *broadcastedMock {
	return &broadcastedMock{
		mut:  sync.Mutex{},
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
	fmt.Println("sent")
	b.msgs = append(b.msgs, msg)
}

func (b *broadcastedMock) Size() int {
	b.mut.Lock()
	defer b.mut.Unlock()

	return len(b.msgs)
}
