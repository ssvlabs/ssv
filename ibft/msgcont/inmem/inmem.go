package inmem

import (
	"encoding/hex"
	"sync"

	"github.com/bloxapp/ssv/ibft/msgcont"
	"github.com/bloxapp/ssv/ibft/proto"
)

// messagesContainer is a simple container for messagesByRound used to count messagesByRound and decide if quorum was achieved.
type messagesContainer struct {
	messagesByRound         map[uint64][]*proto.SignedMessage
	messagesByRoundAndValue map[uint64]map[string][]*proto.SignedMessage // map[round]map[valueHex]msgs
	exitingMsgSigners       map[uint64]map[uint64]bool
	quorumThreshold         uint64
	lock                    sync.RWMutex
}

// New is the constructor of MessagesContainer
func New(quorumThreshold uint64) msgcont.MessageContainer {
	return &messagesContainer{
		messagesByRound:         make(map[uint64][]*proto.SignedMessage),
		messagesByRoundAndValue: make(map[uint64]map[string][]*proto.SignedMessage),
		exitingMsgSigners:       make(map[uint64]map[uint64]bool),
		quorumThreshold:         quorumThreshold,
	}
}

// ReadOnlyMessagesByRound returns messagesByRound by the given round
func (c *messagesContainer) ReadOnlyMessagesByRound(round uint64) []*proto.SignedMessage {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return c.messagesByRound[round]
}

func (c *messagesContainer) readOnlyMessagesByRoundAndValue(round uint64, value []byte) []*proto.SignedMessage {
	c.lock.RLock()
	defer c.lock.RUnlock()
	valueHex := hex.EncodeToString(value)

	if _, found := c.messagesByRoundAndValue[round]; !found {
		return nil
	}
	return c.messagesByRoundAndValue[round][valueHex]
}

func (c *messagesContainer) QuorumAchieved(round uint64, value []byte) (bool, []*proto.SignedMessage) {
	if msgs := c.readOnlyMessagesByRoundAndValue(round, value); msgs != nil {
		signers := 0
		retMsgs := make([]*proto.SignedMessage, 0)
		for _, msg := range msgs {
			signers += len(msg.SignerIds)
			retMsgs = append(retMsgs, msg)
		}

		if uint64(signers) >= c.quorumThreshold {
			return true, retMsgs
		}
	}
	return false, nil
}

// AddMessage adds the given message to the container
func (c *messagesContainer) AddMessage(msg *proto.SignedMessage) {
	c.lock.Lock()
	defer c.lock.Unlock()

	valueHex := hex.EncodeToString(msg.Message.Value)

	// check msg is not duplicate
	if c.exitingMsgSigners[msg.Message.Round] != nil {
		for _, signer := range msg.SignerIds {
			if _, found := c.exitingMsgSigners[msg.Message.Round][signer]; found {
				return
			}
		}
	}

	// add messagesByRound
	_, found := c.messagesByRound[msg.Message.Round]
	if !found {
		c.messagesByRound[msg.Message.Round] = make([]*proto.SignedMessage, 0)
	}
	c.messagesByRound[msg.Message.Round] = append(c.messagesByRound[msg.Message.Round], msg)

	// add messages by round and value
	_, found = c.messagesByRoundAndValue[msg.Message.Round]
	if !found {
		c.messagesByRoundAndValue[msg.Message.Round] = make(map[string][]*proto.SignedMessage)
		c.exitingMsgSigners[msg.Message.Round] = make(map[uint64]bool)
	}
	_, found = c.messagesByRoundAndValue[msg.Message.Round][valueHex]
	if !found {
		c.messagesByRoundAndValue[msg.Message.Round][valueHex] = make([]*proto.SignedMessage, 0)
	}

	for _, signer := range msg.SignerIds {
		c.exitingMsgSigners[msg.Message.Round][signer] = true
	}
	c.messagesByRoundAndValue[msg.Message.Round][valueHex] = append(c.messagesByRoundAndValue[msg.Message.Round][valueHex], msg)
}
