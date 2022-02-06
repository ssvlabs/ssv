package api

import (
	"context"
	"fmt"
	"github.com/prysmaticlabs/prysm/async/event"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"sync"
	"testing"
	"time"
)

func TestConn_Send_FullQueue(t *testing.T) {
	logger := zaptest.NewLogger(t)
	c := newConn(context.Background(), logger, nil, "test", 0, false)

	for i := 0; i < chanSize+2; i++ {
		c.Send([]byte(fmt.Sprintf("test-%d", i)))
	}
}

func TestBroadcaster(t *testing.T) {
	logger := zaptest.NewLogger(t)
	b := newBroadcaster(logger)

	feed := new(event.Feed)
	go func() {
		require.NoError(t, b.FromFeed(feed))
	}()
	bm1 := newBroadcastedMock("1")
	bm2 := newBroadcastedMock("2")

	require.True(t, b.Register(bm1))
	defer b.Deregister(bm1)

	require.True(t, b.Register(bm2))

	// wait so setup will be finished
	<-time.After(10 * time.Millisecond)
	go feed.Send(Message{Type: TypeValidator, Filter: MessageFilter{From: 0, To: 0}})
	<-time.After(5 * time.Millisecond)
	go b.Deregister(bm2)
	<-time.After(5 * time.Millisecond)
	go feed.Send(Message{Type: TypeValidator, Filter: MessageFilter{From: 0, To: 0}})

	// wait so messages will propagate
	<-time.After(100 * time.Millisecond)
	require.Equal(t, bm1.Size(), 2)
	// the second broadcasted was deregistered after the first message
	require.Equal(t, bm2.Size(), 1)
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
