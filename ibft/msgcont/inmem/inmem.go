package inmem

import (
	"sync"

	"github.com/bloxapp/ssv/ibft/msgcont"
	"github.com/bloxapp/ssv/ibft/proto"
)

// messagesContainer is a simple container for messages used to count messages and decide if quorum was achieved.
// TODO - consider moving quorum calculation to MessagesContainer or get rid of it all together
type messagesContainer struct {
	messages map[uint64]map[uint64]*proto.SignedMessage
	lock     sync.Mutex
}

// New is the constructor of MessagesContainer
func New() msgcont.MessageContainer {
	return &messagesContainer{
		messages: make(map[uint64]map[uint64]*proto.SignedMessage),
	}
}

// ReadOnlyMessagesByRound returns messages by the given round
func (c *messagesContainer) ReadOnlyMessagesByRound(round uint64) map[uint64]*proto.SignedMessage {
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.messages[round]
}

// AddMessage adds the given message to the container
func (c *messagesContainer) AddMessage(msg *proto.SignedMessage) {
	c.lock.Lock()
	defer c.lock.Unlock()

	roundMsgs, found := c.messages[msg.Message.Round]
	if !found {
		roundMsgs = make(map[uint64]*proto.SignedMessage)
	}

	// add messages
	for _, id := range msg.SignerIds {
		roundMsgs[id] = msg
		c.messages[msg.Message.Round] = roundMsgs
	}
}
