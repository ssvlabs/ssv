package api

import (
	"encoding/json"
	"sync"

	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/v4/async/event"
	"go.uber.org/zap"
)

// Broadcaster is an interface broadcasting stream message across all available connections
type Broadcaster interface {
	FromFeed(feed *event.Feed) error
	Broadcast(msg Message) error
	Register(conn broadcasted) bool
	Deregister(conn broadcasted) bool
}

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
		mut:         sync.Mutex{},
		connections: map[string]broadcasted{},
	}
}

// FromFeed subscribes to the given feed and broadcasts incoming messages
func (b *broadcaster) FromFeed(msgFeed *event.Feed) error {
	cn := make(chan Message, 512)
	sub := msgFeed.Subscribe(cn)
	defer sub.Unsubscribe()
	defer b.logger.Debug("done reading from feed")

	for {
		select {
		case msg := <-cn:
			go func(msg Message) {
				if err := b.Broadcast(msg); err != nil {
					b.logger.Error("could not broadcast message", zap.Error(err))
				}
			}(msg)
		case err := <-sub.Err():
			b.logger.Warn("could not read messages from msgFeed", zap.Error(err))
			return err
		}
	}
}

// Broadcast broadcasts a message to all available connections
func (b *broadcaster) Broadcast(msg Message) error {
	data, err := json.Marshal(&msg)
	if err != nil {
		return errors.Wrap(err, "could not marshal msg")
	}

	// lock is applied only when reading from the connections map
	// therefore a new temp slice is created to hold all current connections and avoid concurrency issues
	b.mut.Lock()
	var conns []broadcasted
	for _, c := range b.connections {
		conns = append(conns, c)
	}
	b.mut.Unlock()
	// send to all connections
	for _, c := range conns {
		c.Send(data)
	}

	return nil
}

// Register registers a connection for broadcasting
func (b *broadcaster) Register(conn broadcasted) bool {
	b.mut.Lock()
	defer b.mut.Unlock()

	id := conn.ID()
	if _, ok := b.connections[id]; !ok {
		b.connections[id] = conn
		return true
	}
	return false
}

// Deregister de-registers a connection for broadcasting
func (b *broadcaster) Deregister(conn broadcasted) bool {
	b.mut.Lock()
	defer b.mut.Unlock()

	id := conn.ID()
	if _, ok := b.connections[id]; ok {
		delete(b.connections, id)
		return true
	}
	return false
}
