package msgqueue

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sync"
)

// Indexer indexes the given message, returns an empty string if not applicable
// use WithIndexers to inject indexers upon start
type Indexer func(msg *message.SSVMessage) string

// MsgQueue is a message broker for message.SSVMessage
type MsgQueue interface {
	// Add adds a new message
	Add(msg *message.SSVMessage)
	// Purge clears indexed messages for the given index
	Purge(idx string)
	// Peek returns the first n messages for an index
	Peek(idx string, n int) []*message.SSVMessage
	// Pop clears and returns the first n messages for an index
	Pop(idx string, n int) []*message.SSVMessage
	// Count counts messages for the given index
	Count(idx string) int
}

// New creates a new MsgQueue
func New(logger *zap.Logger, opt ...Option) (MsgQueue, error) {
	opts := &Options{}
	if err := opts.Apply(opt...); err != nil {
		return nil, errors.Wrap(err, "could not apply options")
	}

	return &queue{
		logger:    logger,
		indexers:  opts.Indexers,
		itemsLock: &sync.RWMutex{},
		items:     make(map[string][]*msgContainer),
	}, nil
}

type msgContainer struct {
	msg *message.SSVMessage
}

// queue implements MsgQueue
type queue struct {
	logger   *zap.Logger
	indexers []Indexer

	itemsLock *sync.RWMutex
	items     map[string][]*msgContainer
}

func (q *queue) Add(msg *message.SSVMessage) {
	q.itemsLock.Lock()
	defer q.itemsLock.Unlock()

	indexes := q.indexMessage(msg)

	mc := &msgContainer{
		msg: msg,
	}
	for _, idx := range indexes {
		msgs, ok := q.items[idx]
		if !ok {
			msgs = make([]*msgContainer, 0)
		}
		msgs = append(msgs, mc)
		q.items[idx] = msgs
	}
}

func (q *queue) Purge(idx string) {
	q.itemsLock.Lock()
	defer q.itemsLock.Unlock()

	q.items[idx] = make([]*msgContainer, 0)
}

func (q *queue) Peek(idx string, n int) []*message.SSVMessage {
	q.itemsLock.RLock()
	defer q.itemsLock.RUnlock()

	containers, ok := q.items[idx]
	if !ok {
		return nil
	}

	if n == 0 {
		n = len(containers)
	}
	containers = containers[:n]
	msgs := make([]*message.SSVMessage, 0)
	for _, mc := range containers {
		msgs = append(msgs, mc.msg)
	}

	return msgs
}

// Pop clears and returns the first n messages for an index
func (q *queue) Pop(idx string, n int) []*message.SSVMessage {
	q.itemsLock.Lock()
	defer q.itemsLock.Unlock()

	containers, ok := q.items[idx]
	if !ok {
		return nil
	}
	if n == 0 || n > len(containers) {
		n = len(containers)
	}
	q.items[idx] = containers[n:]
	containers = containers[:n]
	msgs := make([]*message.SSVMessage, 0)
	for _, mc := range containers {
		msgs = append(msgs, mc.msg)
	}

	return msgs
}

func (q *queue) Count(idx string) int {
	q.itemsLock.RLock()
	defer q.itemsLock.RUnlock()

	containers, ok := q.items[idx]
	if !ok {
		return 0
	}
	return len(containers)
}

// indexMessage returns indexes for the given message.
// NOTE: this function is not thread safe
func (q *queue) indexMessage(msg *message.SSVMessage) []string {
	indexes := make([]string, 0)
	for _, f := range q.indexers {
		idx := f(msg)
		if len(idx) > 0 {
			indexes = append(indexes, idx)
		}
	}

	return indexes
}
