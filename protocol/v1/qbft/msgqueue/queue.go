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
	// Add adds a new message to the queue for the matching indices
	Add(msg *message.SSVMessage)
	// Peek returns the first n messages for an index
	Peek(n int, idx Index) []*message.SSVMessage
	// Pop clears and returns the first n messages for an index
	Pop(n int, idx Index) []*message.SSVMessage
	// PopIndices clears and returns the first n messages for indices that are created on demand using the iterator
	PopIndices(n int, generator *IndexIterator) []*message.SSVMessage
	// Purge clears indexed messages for the given index
	Purge(idx Index) int64
	// Clean enables to aid in a custom Cleaner to clear any set of indices
	// TODO: check performance
	Clean(cleaners ...Cleaner) int64
	// Count counts messages for the given index
	Count(idx Index) int
	// Len counts all messages
	Len() int
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

// Index is a struct representing an index in msg queue
type Index struct {
	Name string
	// Mt is the message type
	Mt message.MsgType
	// ID is the identifier
	ID string
	// H (optional) is the height, -1 is treated as nil
	H message.Height
	// Cmt (optional) is the consensus msg type, -1 is treated as nil
	Cmt message.ConsensusMessageType
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
		metricsMsgQRatio.WithLabelValues(idx.ID, idx.Name, idx.Mt.String(), idx.Cmt.String()).Inc()
	}
	q.logger.Debug("message added to queue", zap.Any("indices", indices))
}

func (q *queue) Purge(idx Index) int64 {
	q.itemsLock.Lock()
	defer q.itemsLock.Unlock()

	size := len(q.items[idx])
	delete(q.items, idx)
	metricsMsgQRatio.WithLabelValues(idx.ID, idx.Name, idx.Mt.String(), idx.Cmt.String()).Sub(float64(size))

	return int64(size)
}

func (q *queue) Clean(cleaners ...Cleaner) int64 {
	q.itemsLock.Lock()
	defer q.itemsLock.Unlock()

	var cleaned int64

	apply := func(idx Index) bool {
		for _, cleaner := range cleaners {
			if cleaner(idx) {
				size := len(q.items[idx])
				atomic.AddInt64(&cleaned, int64(size))
				metricsMsgQRatio.WithLabelValues(idx.ID, idx.Name, idx.Mt.String(), idx.Cmt.String()).Sub(float64(size))
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

func (q *queue) Peek(n int, idx Index) []*message.SSVMessage {
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

// Pop messages by index with a desired amount of messages to pop,
// defaults to the all the messages in the index.
func (q *queue) Pop(n int, idx Index) []*message.SSVMessage {
	q.itemsLock.Lock()
	defer q.itemsLock.Unlock()

	msgs := make([]*message.SSVMessage, 0)

	msgContainers, ok := q.items[idx]
	if !ok {
		return msgs
	}
	if n == 0 || n > len(msgContainers) {
		n = len(msgContainers)
	}
	q.items[idx] = msgContainers[n:]
	if len(q.items[idx]) == 0 {
		delete(q.items, idx)
	}
	msgContainers = msgContainers[:n]
	for _, mc := range msgContainers {
		if mc.msg != nil {
			msgs = append(msgs, mc.msg)
		}
	}
	if nMsgs := len(msgs); nMsgs > 0 {
		metricsMsgQRatio.WithLabelValues(idx.ID, idx.Name, idx.Mt.String(), idx.Cmt.String()).Sub(float64(nMsgs))
	}
	return msgs
}

func (q *queue) PopIndices(n int, i *IndexIterator) []*message.SSVMessage {
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
		results := q.Pop(n-len(msgs), idx)
		if len(results) > 0 {
			msgs = append(msgs, results...)
		}
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
		return k.ID == identifier
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
		Mt:  mt,
		ID:  mid.String(),
		Cmt: -1,
	}
}
