package msgqueue

import (
	"sync"

	"github.com/pborman/uuid"

	"github.com/bloxapp/ssv/ibft/proto"
)

// SubscribeFunc is the function that triggers once new message received
type SubscribeFunc func(*proto.SignedMessage)

// MessageQueue is a broker of messages for the IBFT instance to process.
// Messages can come in various times, even next round's messages can come "early" as other nodes can change round before this node.
// To solve this issue we have a message broker from which the instance pulls new messages, this also reduces concurrency issues as the instance is now single threaded.
// The message queue has internal logic to organize messages by their round.
type MessageQueue struct {
	msgMutex         sync.Mutex
	subscriptions    map[string]SubscribeFunc
	currentRound     uint64
	futureRoundQueue []*proto.SignedMessage
}

// New is the constructor of MessageQueue
func New() *MessageQueue {
	return &MessageQueue{
		msgMutex:         sync.Mutex{},
		subscriptions:    make(map[string]SubscribeFunc),
		futureRoundQueue: make([]*proto.SignedMessage, 0),
	}
}

// AddMessage adds a message the queue based on the message round.
// AddMessage is thread safe
func (q *MessageQueue) AddMessage(msg *proto.SignedMessage) {
	q.msgMutex.Lock()
	defer q.msgMutex.Unlock()

	if msg.Message.Round < q.currentRound {
		return // not adding previous round messages
	}

	if q.currentRound != msg.Message.Round {
		q.futureRoundQueue = append(q.futureRoundQueue, msg)
		return
	}

	if q.subscriptions == nil {
		return
	}

	for _, sub := range q.subscriptions {
		go sub(msg)
	}
}

// Subscribe creates a new subscription and puts it to the list
func (q *MessageQueue) Subscribe(fn SubscribeFunc) func() {
	q.msgMutex.Lock()
	defer q.msgMutex.Unlock()

	if q.subscriptions == nil {
		q.subscriptions = make(map[string]SubscribeFunc)
	}

	id := uuid.New()
	q.subscriptions[id] = fn

	return func() {
		q.msgMutex.Lock()
		delete(q.subscriptions, id)
		q.msgMutex.Unlock()
	}
}

// SetRound validates and sets round depending on message current and future round
func (q *MessageQueue) SetRound(newRound uint64) {
	q.msgMutex.Lock()
	defer q.msgMutex.Unlock()

	// set round
	q.currentRound = newRound

	// move from future round to current round, also remove dead messages
	newFutureRoundQueue := make([]*proto.SignedMessage, 0)
	for _, msg := range q.futureRoundQueue {
		if msg.Message.Round < q.currentRound {
			// do nothing, will delete this message
		} else if msg.Message.Round == q.currentRound {
			for _, sub := range q.subscriptions {
				go sub(msg)
			}
		} else { //  msg.Message.Round > q.currentRound
			newFutureRoundQueue = append(newFutureRoundQueue, msg)
		}
	}
	q.futureRoundQueue = newFutureRoundQueue
}
