package api

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/prysmaticlabs/prysm/v4/async/event"
	"go.uber.org/zap"
)

// Broadcaster is an interface broadcasting stream messages to all available connections.
type Broadcaster interface {
	FromFeed(ctx context.Context, feed *event.Feed)
	Broadcast(msg Message) error
	Register(conn broadcasted) bool
	Deregister(conn broadcasted) bool
}

// broadcasted is the contract a connection must satisfy to be registered.
//
// Send MUST NOT block. The broadcaster fans out by calling Send synchronously
// for every registered connection, so a blocking Send would back-pressure
// every other client. Implementations are expected to drop or self-close on
// their own queue overflow (see conn.Send for the canonical pattern: a
// non-blocking select+default that cancels the conn ctx when the queue
// fills, so the slow client reconnects with a fresh view rather than
// silently missing messages).
type broadcasted interface {
	ID() string
	Send([]byte)
}

type broadcaster struct {
	logger      *zap.Logger
	mut         sync.Mutex
	connections map[string]broadcasted
}

func newBroadcaster(logger *zap.Logger) Broadcaster {
	return &broadcaster{
		logger:      logger,
		connections: map[string]broadcasted{},
	}
}

// FromFeed subscribes to the given feed synchronously so any subsequent Send
// on the feed is guaranteed to reach this broadcaster, then spawns a pump
// goroutine that runs Broadcast inline until ctx is canceled. Broadcast is
// bounded-fast (marshal once + N non-blocking c.Send calls), so the feed
// buffer drains in microseconds and synchronous publishers (e.g. decideds)
// don't back-pressure on stream clients.
func (b *broadcaster) FromFeed(ctx context.Context, msgFeed *event.Feed) {
	buffer := make(chan Message, 512)
	sub := msgFeed.Subscribe(buffer)
	go func() {
		defer sub.Unsubscribe()
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-buffer:
				if err := b.Broadcast(msg); err != nil {
					b.logger.Error("could not broadcast message", zap.Error(err))
				}
			}
		}
	}()
}

// Broadcast marshals msg once and hands it to every registered connection
// via broadcasted.Send. Send is required to be non-blocking (see the
// broadcasted contract), so the lock-held fan-out is bounded fast.
func (b *broadcaster) Broadcast(msg Message) error {
	data, err := json.Marshal(&msg)
	if err != nil {
		return fmt.Errorf("could not marshal msg: %w", err)
	}

	b.mut.Lock()
	defer b.mut.Unlock()
	for _, c := range b.connections {
		c.Send(data)
	}
	return nil
}

// Register adds conn to the broadcast set. Returns false if a connection
// with this id is already registered.
func (b *broadcaster) Register(conn broadcasted) bool {
	b.mut.Lock()
	defer b.mut.Unlock()

	id := conn.ID()
	if _, ok := b.connections[id]; ok {
		return false
	}
	b.connections[id] = conn
	return true
}

// Deregister removes conn from the broadcast set.
func (b *broadcaster) Deregister(conn broadcasted) bool {
	b.mut.Lock()
	defer b.mut.Unlock()

	id := conn.ID()
	if _, ok := b.connections[id]; !ok {
		return false
	}
	delete(b.connections, id)
	return true
}
