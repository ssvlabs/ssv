package api

import "sync"

type msgQueue struct {
	mut     sync.Mutex
	msgs    []*NetworkMessage
	running bool
}

func newMsgQ() *msgQueue {
	return &msgQueue{
		sync.Mutex{},
		make([]*NetworkMessage, 0),
		true,
	}
}

func (mq *msgQueue) enqueue(nm *NetworkMessage) bool {
	mq.mut.Lock()
	defer mq.mut.Unlock()

	if !mq.running {
		return false
	}
	if len(mq.msgs) < msgQueueLimit {
		mq.msgs = append(mq.msgs, nm)
		return true
	}
	return false
}

func (mq *msgQueue) dequeue() (*NetworkMessage, bool) {
	mq.mut.Lock()
	defer mq.mut.Unlock()

	if !mq.running {
		return nil, false
	}

	if len(mq.msgs) == 0 {
		return nil, true
	}
	nm := mq.msgs[0]
	mq.msgs = mq.msgs[1:]

	return nm, true
}

func (mq *msgQueue) stop() {
	mq.mut.Lock()
	defer mq.mut.Unlock()

	mq.running = false
}
