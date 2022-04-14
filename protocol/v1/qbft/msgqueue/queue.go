package msgqueue

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// Cleaner is a function for iterating over keys and clean irrelevant ones
type Cleaner func(string) bool

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
	// Clean will clean irrelevant keys from the map
	// TODO: check performance
	Clean(cleaner Cleaner) int
}

// New creates a new MsgQueue
func New(logger *zap.Logger, opt ...Option) (MsgQueue, error) {
	var err error

	opts := &Options{}
	if err = opts.Apply(opt...); err != nil {
		err = errors.Wrap(err, "could not apply options")
	}

	return &queue{
		logger:    logger,
		indexers:  opts.Indexers,
		itemsLock: &sync.RWMutex{},
		items:     make(map[string][]*msgContainer),
	}, err
}

type msgContainer struct {
	msg *message.SSVMessage
}

// queue implements MsgQueue
type queue struct {
	ctx      context.Context
	logger   *zap.Logger
	indexers []Indexer

	itemsLock *sync.RWMutex
	items     map[string][]*msgContainer
}

func (q *queue) Add(msg *message.SSVMessage) {
	q.itemsLock.Lock()
	defer q.itemsLock.Unlock()

	indices := q.indexMessage(msg)

	mc := &msgContainer{
		msg: msg,
	}
	for _, idx := range indices {
		if len(idx) == 0 {
			continue
		}
		msgs, ok := q.items[idx]
		if !ok {
			msgs = make([]*msgContainer, 0)
		}
		msgs = ByConsensusMsgType().Combine(ByRound()).Add(msgs, mc)
		q.items[idx] = msgs
	}
}

func (q *queue) Purge(idx string) {
	q.itemsLock.Lock()
	defer q.itemsLock.Unlock()

	q.items[idx] = make([]*msgContainer, 0)
}

func (q *queue) Clean(cleaner Cleaner) int {
	q.itemsLock.Lock()
	defer q.itemsLock.Unlock()

	cleaned := 0
	for k := range q.items {
		if cleaner(k) {
			cleaned += len(q.items[k])
			delete(q.items, k)
		}
	}
	return cleaned
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

// DefaultMsgCleaner cleans ssv msgs from the queue
func DefaultMsgCleaner(mt message.MsgType, mid message.Identifier) Cleaner {
	return func(k string) bool {
		parts := strings.Split(k, "/")
		if len(parts) < 2 {
			return false // unknown
		}
		parts = parts[1:]
		if parts[0] != mt.String() {
			return false
		}
		if parts[2] != fmt.Sprintf("%x", mid) {
			return false
		}
		// clean
		return true
	}
}

// DefaultMsgIndexer returns the default msg indexer to use for message.SSVMessage
func DefaultMsgIndexer() Indexer {
	return func(msg *message.SSVMessage) string {
		if msg == nil {
			return ""
		}
		return DefaultMsgIndex(msg.MsgType, msg.ID)
	}
}

// DefaultMsgIndex is the default msg index
func DefaultMsgIndex(mt message.MsgType, mid message.Identifier) string {
	return fmt.Sprintf("/%s/id/%x", mt.String(), mid)
}
