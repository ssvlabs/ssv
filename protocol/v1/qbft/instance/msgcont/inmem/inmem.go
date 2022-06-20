package inmem

import (
	"encoding/hex"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/msgcont"
	"sync"

	"github.com/bloxapp/ssv/protocol/v1/message"
)

// messagesContainer is a simple container for messagesByRound used to count messagesByRound and decide if quorum was achieved.
type messagesContainer struct {
	messagesByRound         map[message.Round][]*message.SignedMessage
	messagesByRoundAndValue map[message.Round]map[string][]*message.SignedMessage // map[round]map[valueHex]msgs
	allChangeRoundMessages  []*message.SignedMessage
	exitingMsgSigners       map[message.Round]map[message.OperatorID]bool
	quorumThreshold         uint64
	partialQuorumThreshold  uint64
	lock                    sync.RWMutex
}

// New is the constructor of MessagesContainer
func New(quorumThreshold, partialQuorumThreshold uint64) msgcont.MessageContainer {
	return &messagesContainer{
		messagesByRound:         make(map[message.Round][]*message.SignedMessage),
		messagesByRoundAndValue: make(map[message.Round]map[string][]*message.SignedMessage),
		allChangeRoundMessages:  make([]*message.SignedMessage, 0),
		exitingMsgSigners:       make(map[message.Round]map[message.OperatorID]bool),
		quorumThreshold:         quorumThreshold,
		partialQuorumThreshold:  partialQuorumThreshold,
	}
}

// AllMessaged returns all messages
func (c *messagesContainer) AllMessaged(iterator func(k message.Round, v *message.SignedMessage)) []*message.SignedMessage {
	ret := make([]*message.SignedMessage, 0)
	for round, roundMsgs := range c.messagesByRound {
		for _, msg := range roundMsgs {
			iterator(round, msg)
		}
	}
	return ret
}

// ReadOnlyMessagesByRound returns messagesByRound by the given round
func (c *messagesContainer) ReadOnlyMessagesByRound(round message.Round) []*message.SignedMessage {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return c.messagesByRound[round]
}

func (c *messagesContainer) readOnlyMessagesByRoundAndValue(round message.Round, value []byte) []*message.SignedMessage {
	c.lock.RLock()
	defer c.lock.RUnlock()
	valueHex := hex.EncodeToString(value)

	if _, found := c.messagesByRoundAndValue[round]; !found {
		return nil
	}
	return c.messagesByRoundAndValue[round][valueHex]
}

func (c *messagesContainer) QuorumAchieved(round message.Round, value []byte) (bool, []*message.SignedMessage) {
	if msgs := c.readOnlyMessagesByRoundAndValue(round, value); msgs != nil {
		signers := 0
		retMsgs := make([]*message.SignedMessage, 0)
		for _, msg := range msgs {
			signers += len(msg.GetSigners())
			retMsgs = append(retMsgs, msg)
		}

		if uint64(signers) >= c.quorumThreshold {
			return true, retMsgs
		}
	}
	return false, nil
}

// AddMessage adds the given message to the container
func (c *messagesContainer) AddMessage(msg *message.SignedMessage, data []byte) {
	c.lock.Lock()
	defer c.lock.Unlock()

	valueHex := hex.EncodeToString(data)

	// check msg is not duplicate
	if c.exitingMsgSigners[msg.Message.Round] != nil {
		for _, signer := range msg.GetSigners() {
			if _, found := c.exitingMsgSigners[msg.Message.Round][signer]; found {
				return
			}
		}
	}

	// add messagesByRound
	_, found := c.messagesByRound[msg.Message.Round]
	if !found {
		c.messagesByRound[msg.Message.Round] = make([]*message.SignedMessage, 0)
	}
	c.messagesByRound[msg.Message.Round] = append(c.messagesByRound[msg.Message.Round], msg)

	// add messages by round and value
	_, found = c.messagesByRoundAndValue[msg.Message.Round]
	if !found {
		c.messagesByRoundAndValue[msg.Message.Round] = make(map[string][]*message.SignedMessage)
		c.exitingMsgSigners[msg.Message.Round] = make(map[message.OperatorID]bool)
	}
	_, found = c.messagesByRoundAndValue[msg.Message.Round][valueHex]
	if !found {
		c.messagesByRoundAndValue[msg.Message.Round][valueHex] = make([]*message.SignedMessage, 0)
	}

	// add to all change round messages
	if msg.Message.MsgType == message.RoundChangeMsgType {
		c.allChangeRoundMessages = append(c.allChangeRoundMessages, msg)
	}

	for _, signer := range msg.GetSigners() {
		c.exitingMsgSigners[msg.Message.Round][signer] = true
	}
	c.messagesByRoundAndValue[msg.Message.Round][valueHex] = append(c.messagesByRoundAndValue[msg.Message.Round][valueHex], msg)
}

// OverrideMessages will override all current msgs in container with the provided msg
func (c *messagesContainer) OverrideMessages(msg *message.SignedMessage, data []byte) {
	c.lock.Lock()
	// reset previous round data
	delete(c.exitingMsgSigners, msg.Message.Round)
	delete(c.messagesByRound, msg.Message.Round)
	delete(c.messagesByRoundAndValue, msg.Message.Round)
	c.lock.Unlock()

	// override
	c.AddMessage(msg, data)
}

func (c *messagesContainer) PartialChangeRoundQuorum(stateRound message.Round) (found bool, lowestChangeRound message.Round) {
	lowestChangeRound = message.Round(100000) // just a random really large round number
	foundMsgs := make(map[message.OperatorID]*message.SignedMessage)
	quorumCount := 0

	c.lock.RLock()
	defer c.lock.RUnlock()

	for _, msg := range c.allChangeRoundMessages {
		if msg.Message.Round <= stateRound {
			continue
		}

		for _, signer := range msg.GetSigners() {
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
