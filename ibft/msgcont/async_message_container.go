package msgcont

import (
	"sync"

	"github.com/bloxapp/ssv/ibft/proto"
)

// MessagesContainer is a simple container for messages used to count messages and decide if quorum was achieved.
// TODO - consider moving quorum calculation to MessagesContainer or get rid of it all together
type MessagesContainer struct {
	messages map[uint64]map[uint64]proto.SignedMessage
	lock     sync.Mutex
}

func NewMessagesContainer() *MessagesContainer {
	return &MessagesContainer{
		messages: make(map[uint64]map[uint64]proto.SignedMessage),
		lock:     sync.Mutex{},
	}
}

func (c *MessagesContainer) ReadOnlyMessagesByRound(round uint64) map[uint64]proto.SignedMessage {
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.messages[round]
}

func (c *MessagesContainer) AddMessage(msg proto.SignedMessage) {
	c.lock.Lock()
	defer c.lock.Unlock()

	roundMsgs, found := c.messages[msg.Message.Round]
	if !found {
		roundMsgs = make(map[uint64]proto.SignedMessage)
	}
	roundMsgs[msg.IbftId] = msg
	c.messages[msg.Message.Round] = roundMsgs
}
