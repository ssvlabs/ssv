package types

import "sync"

type MessagesContainer struct {
	messages map[uint64]map[uint64]Message
	lock     sync.Mutex
}

func NewMessagesContainer() *MessagesContainer {
	return &MessagesContainer{
		messages: make(map[uint64]map[uint64]Message),
		lock:     sync.Mutex{},
	}
}

func (c *MessagesContainer) ReadOnlyMessagesByRound(round uint64) map[uint64]Message {
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.messages[round]
}

func (c *MessagesContainer) AddMessage(msg Message) {
	c.lock.Lock()
	defer c.lock.Unlock()

	roundMsgs, found := c.messages[msg.Round]
	if !found {
		roundMsgs = make(map[uint64]Message)
	}
	roundMsgs[msg.IbftId] = msg
	c.messages[msg.Round] = roundMsgs
}
