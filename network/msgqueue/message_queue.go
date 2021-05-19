package msgqueue

import (
	"github.com/bloxapp/ssv/network"
	"github.com/pborman/uuid"
	"sync"
)

// IndexFunc is the function that indexes messages to be later pulled by those indexes
type IndexFunc func(msg *network.Message) []string

type messageContainer struct {
	id      string
	msg     *network.Message
	indexes []string
}

// MessageQueue is a broker of messages for the IBFT instance to process.
// Messages can come in various times, even next round's messages can come "early" as other nodes can change round before this node.
// To solve this issue we have a message broker from which the instance pulls new messages, this also reduces concurrency issues as the instance is now single threaded.
// The message queue has internal logic to organize messages by their round.
type MessageQueue struct {
	msgMutex   sync.Mutex
	indexFuncs []IndexFunc
	queue      map[string][]*messageContainer // = map[index][messageContainer.id]messageContainer
}

// New is the constructor of MessageQueue
func New() *MessageQueue {
	return &MessageQueue{
		msgMutex: sync.Mutex{},
		queue:    make(map[string][]*messageContainer),
		indexFuncs: []IndexFunc{
			iBFTMessageIndex(),
			sigMessageIndex(),
		},
	}
}

// AddIndexFunc adds an index function that will be activated every new message the queue receives
func (q *MessageQueue) AddIndexFunc(f IndexFunc) {
	q.indexFuncs = append(q.indexFuncs, f)
}

// AddMessage adds a message the queue based on the message round.
// AddMessage is thread safe
func (q *MessageQueue) AddMessage(msg *network.Message) {
	q.msgMutex.Lock()
	defer q.msgMutex.Unlock()

	// index msg
	indexes := make([]string, 0)
	for _, f := range q.indexFuncs {
		indexes = append(indexes, f(msg)...)
	}

	// add it to queue
	msgContainer := &messageContainer{
		id:      uuid.New(),
		msg:     msg,
		indexes: indexes,
	}

	for _, idx := range indexes {
		if q.queue[idx] == nil {
			q.queue[idx] = make([]*messageContainer, 0)
		}
		q.queue[idx] = append(q.queue[idx], msgContainer)
	}
}

// PopMessage will return a message by its index if found, will also delete all other index occurrences of that message
func (q *MessageQueue) PopMessage(index string) *network.Message {
	q.msgMutex.Lock()
	defer q.msgMutex.Unlock()

	var ret *network.Message
	if len(q.queue[index]) > 0 {
		c := q.queue[index][0]
		ret = c.msg

		// delete all indexes
		q.deleteMessageFromAllIndexes(c.indexes, c.id)
	}
	return ret
}

// MsgCount will return a count of messages by their index
func (q *MessageQueue) MsgCount(index string) int {
	return len(q.queue[index])
}

func (q *MessageQueue) deleteMessageFromAllIndexes(indexes []string, id string) {
	for _, indx := range indexes {
		newIndexQ := make([]*messageContainer, 0)
		for _, msg := range q.queue[indx] {
			if msg.id != id {
				newIndexQ = append(newIndexQ, msg)
			}
		}
		q.queue[indx] = newIndexQ
	}
}

// PurgeIndexedMessages will delete all indexed messages for the given index
func (q *MessageQueue) PurgeIndexedMessages(index string) {
	q.msgMutex.Lock()
	defer q.msgMutex.Unlock()

	q.queue[index] = make([]*messageContainer, 0)
}
