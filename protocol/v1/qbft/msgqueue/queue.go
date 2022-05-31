package msgqueue

import (
	"sync"
	"sync/atomic"

	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// Cleaner is a function for iterating over keys and clean irrelevant ones
type Cleaner func(Index) bool

// AllIndicesCleaner is a cleaner that removes all existing indices
func AllIndicesCleaner(k Index) bool {
	return true
}

// Indexer indexes the given message, returns an empty string if not applicable
// use WithIndexers to inject indexers upon start
type Indexer func(msg *message.SSVMessage) Index

// MsgQueue is a message broker for message.SSVMessage
type MsgQueue interface {
	// Add adds a new message
	Add(msg *message.SSVMessage)
	// Purge clears indexed messages for the given index
	Purge(idx Index)
	// Peek returns the first n messages for an index
	Peek(idx Index, n int) []*message.SSVMessage
	// Pop clears and returns the first n messages for an index
	Pop(n int, idx ...Index) []*message.SSVMessage
	// PopWithIterator clears and returns the first n messages for indices that are created on the fly
	PopWithIterator(n int, generator *IndexIterator) []*message.SSVMessage
	// Count counts messages for the given index
	Count(idx Index) int
	// Len counts all messages
	Len() int
	// Clean will clean irrelevant keys from the map
	// TODO: check performance
	Clean(cleaners ...Cleaner) int64
}

// New creates a new MsgQueue
func New(logger *zap.Logger, opt ...Option) (MsgQueue, error) {
	var err error
	opts := &Options{}
	if err = opts.Apply(opt...); err != nil {
		err = errors.Wrap(err, "could not apply options")
	}
	if err != nil {
		return nil, err
	}

	logger.Debug("queue configuration", zap.Any("indexers", len(opts.Indexers)))

	return &queue{
		logger:    logger,
		indexers:  opts.Indexers,
		itemsLock: &sync.RWMutex{},
		items:     make(map[Index][]*MsgContainer),
	}, err
}

// MsgContainer is a container for a message
type MsgContainer struct {
	msg *message.SSVMessage
}

type Index struct {
	Mt         message.MsgType
	Identifier string
	H          message.Height               // -1 count as nil
	Cmt        message.ConsensusMessageType // -1 count as nil
}

// queue implements MsgQueue
type queue struct {
	logger   *zap.Logger
	indexers []Indexer

	itemsLock *sync.RWMutex
	items     map[Index][]*MsgContainer
}

func (q *queue) Add(msg *message.SSVMessage) {
	q.itemsLock.Lock()
	defer q.itemsLock.Unlock()

	indices := q.indexMessage(msg)
	mc := &MsgContainer{
		msg: msg,
	}
	for _, idx := range indices {
		if idx == (Index{}) {
			continue
		}
		msgs, ok := q.items[idx]
		if !ok {
			msgs = make([]*MsgContainer, 0)
		}
		msgs = ByConsensusMsgType().Combine(ByRound()).Add(msgs, mc)
		q.items[idx] = msgs
	}
	q.logger.Debug("message added to queue", zap.Any("indices", indices))
}

func (q *queue) Purge(idx Index) {
	q.itemsLock.Lock()
	defer q.itemsLock.Unlock()

	q.items[idx] = make([]*MsgContainer, 0)
}

func (q *queue) Clean(cleaners ...Cleaner) int64 {
	q.itemsLock.Lock()
	defer q.itemsLock.Unlock()

	var cleaned int64

	apply := func(k Index) bool {
		for _, cleaner := range cleaners {
			if cleaner(k) {
				atomic.AddInt64(&cleaned, int64(len(q.items[k])))
				return true
			}
		}
		return false
	}

	for k := range q.items {
		if apply(k) {
			delete(q.items, k)
		}
	}

	return cleaned
}

func (q *queue) Peek(idx Index, n int) []*message.SSVMessage {
	q.itemsLock.RLock()
	defer q.itemsLock.RUnlock()

	containers, ok := q.items[idx]
	if !ok {
		return nil
	}
	if n == 0 || n > len(containers) {
		n = len(containers)
	}
	containers = containers[:n]
	msgs := make([]*message.SSVMessage, 0)
	for _, mc := range containers {
		msgs = append(msgs, mc.msg)
	}

	return msgs
}

// Pop message by index. if no messages found within the index and more than 1 idxs passed, search on the next one.
func (q *queue) Pop(n int, idxs ...Index) []*message.SSVMessage {
	q.itemsLock.Lock()
	defer q.itemsLock.Unlock()
	
	for i, idx := range idxs {
		containers, ok := q.items[idx]
		if !ok {
			return []*message.SSVMessage{}
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

		// check if there are more idxs to search for and if msgs not found
		if i < len(idxs)-1 && (len(msgs) == 0 || msgs[0] == nil) {
			continue // move to next idx
		}
		return msgs
	}
	return []*message.SSVMessage{}
}

func (q *queue) PopWithIterator(n int, i *IndexIterator) []*message.SSVMessage {
	var msgs []*message.SSVMessage

	for len(msgs) < n {
		genIndex := i.Next()
		if genIndex == nil {
			break
		}
		idx := genIndex()
		if idx == (Index{}) {
			continue
		}
		results := q.Pop(n, genIndex())
		if len(results) > 0 {
			msgs = append(msgs, results...)
		}
	}
	if len(msgs) > n {
		msgs = msgs[:n]
	}
	return msgs
}

func (q *queue) Count(idx Index) int {
	q.itemsLock.RLock()
	defer q.itemsLock.RUnlock()

	containers, ok := q.items[idx]
	if !ok {
		return 0
	}
	return len(containers)
}

func (q *queue) Len() int {
	q.itemsLock.RLock()
	defer q.itemsLock.RUnlock()
	return len(q.items)
}

// indexMessage returns indexes for the given message.
// NOTE: this function is not thread safe
func (q *queue) indexMessage(msg *message.SSVMessage) []Index {
	indexes := make([]Index, 0)
	for _, f := range q.indexers {
		idx := f(msg)
		if idx != (Index{}) {
			indexes = append(indexes, idx)
		}
	}

	return indexes
}

// DefaultMsgCleaner cleans ssv msgs from the queue
func DefaultMsgCleaner(mid message.Identifier, mts ...message.MsgType) Cleaner {
	identifier := mid.String()
	return func(k Index) bool {
		for _, mt := range mts {
			if k.Mt != mt {
				return false
			}
		}
		// clean if we reached here, and the identifier is equal
		return k.Identifier == identifier
	}
}

// DefaultMsgIndexer returns the default msg indexer to use for message.SSVMessage
func DefaultMsgIndexer() Indexer {
	return func(msg *message.SSVMessage) Index {
		if msg == nil {
			return Index{}
		}
		return DefaultMsgIndex(msg.MsgType, msg.ID)
	}
}

// DefaultMsgIndex is the default msg index
func DefaultMsgIndex(mt message.MsgType, mid message.Identifier) Index {
	return Index{
		Mt:         mt,
		Identifier: mid.String(),
		Cmt:        -1,
	}
}
