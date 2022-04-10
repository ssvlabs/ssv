package inmem

import (
	"encoding/hex"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"sync"

	"github.com/bloxapp/ssv/ibft/instance/msgcont"
	"github.com/bloxapp/ssv/ibft/proto"
)

// messagesContainer is a simple container for messagesByRound used to count messagesByRound and decide if quorum was achieved.
type messagesContainer struct {
	messagesByRound         map[uint64][]*proto.SignedMessage
	messagesByRoundAndValue map[uint64]map[string][]*proto.SignedMessage // map[round]map[valueHex]msgs
	allChangeRoundMessages  []*proto.SignedMessage
	exitingMsgSigners       map[uint64]map[uint64]bool
	quorumThreshold         uint64
	partialQuorumThreshold  uint64
	lock                    sync.RWMutex
}

// New is the constructor of MessagesContainer
func New(quorumThreshold, partialQuorumThreshold uint64) msgcont.MessageContainer {
	return &messagesContainer{
		messagesByRound:         make(map[uint64][]*proto.SignedMessage),
		messagesByRoundAndValue: make(map[uint64]map[string][]*proto.SignedMessage),
		allChangeRoundMessages:  make([]*proto.SignedMessage, 0),
		exitingMsgSigners:       make(map[uint64]map[uint64]bool),
		quorumThreshold:         quorumThreshold,
		partialQuorumThreshold:  partialQuorumThreshold,
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
func (c *messagesContainer) AddMessage(msg *message.SignedMessage) {
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

	// add to all change round messages
	if msg.Message.Type == proto.RoundState_ChangeRound {
		c.allChangeRoundMessages = append(c.allChangeRoundMessages, msg)
	}

	for _, signer := range msg.SignerIds {
		c.exitingMsgSigners[msg.Message.Round][signer] = true
	}
	c.messagesByRoundAndValue[msg.Message.Round][valueHex] = append(c.messagesByRoundAndValue[msg.Message.Round][valueHex], msg)
}

// OverrideMessages will override all current msgs in container with the provided msg
func (c *messagesContainer) OverrideMessages(msg *proto.SignedMessage) {
	c.lock.Lock()
	// reset previous round data
	delete(c.exitingMsgSigners, msg.Message.Round)
	delete(c.messagesByRound, msg.Message.Round)
	delete(c.messagesByRoundAndValue, msg.Message.Round)
	c.lock.Unlock()

	// override
	c.AddMessage(msg)
}

func (c *messagesContainer) PartialChangeRoundQuorum(stateRound uint64) (found bool, lowestChangeRound uint64) {
	lowestChangeRound = uint64(100000) // just a random really large round number
	foundMsgs := make(map[uint64]*proto.SignedMessage)
	quorumCount := 0

	c.lock.RLock()
	defer c.lock.RUnlock()

	for _, msg := range c.allChangeRoundMessages {
		if msg.Message.Round <= stateRound {
			continue
		}

		for _, signer := range msg.SignerIds {
			if existingMsg, found := foundMsgs[signer]; found {
				if existingMsg.Message.Round > msg.Message.Round {
					foundMsgs[signer] = msg
				}
			} else {
				foundMsgs[signer] = msg
				quorumCount++
			}

			// recalculate lowest
			if foundMsgs[signer].Message.Round < lowestChangeRound {
				lowestChangeRound = msg.Message.Round
			}
		}
	}

	return quorumCount >= int(c.partialQuorumThreshold), lowestChangeRound
}
