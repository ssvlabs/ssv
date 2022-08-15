package inmem

import (
	"bytes"
	"encoding/hex"
	"sync"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/msgcont"
)

// messagesContainer is a simple container for messagesByRound used to count messagesByRound and decide if quorum was achieved.
type messagesContainer struct {
	messagesByRound         map[specqbft.Round][]*specqbft.SignedMessage
	messagesByRoundAndValue map[specqbft.Round]map[string][]*specqbft.SignedMessage // map[round]map[valueHex]msgs
	allChangeRoundMessages  []*specqbft.SignedMessage
	quorumThreshold         uint64
	partialQuorumThreshold  uint64
	lock                    sync.RWMutex
}

// New is the constructor of MessagesContainer
func New(quorumThreshold, partialQuorumThreshold uint64) msgcont.MessageContainer {
	return &messagesContainer{
		messagesByRound:         make(map[specqbft.Round][]*specqbft.SignedMessage),
		messagesByRoundAndValue: make(map[specqbft.Round]map[string][]*specqbft.SignedMessage),
		allChangeRoundMessages:  make([]*specqbft.SignedMessage, 0),
		quorumThreshold:         quorumThreshold,
		partialQuorumThreshold:  partialQuorumThreshold,
	}
}

// AllMessaged returns all messages
func (c *messagesContainer) AllMessaged(iterator func(k specqbft.Round, v *specqbft.SignedMessage)) []*specqbft.SignedMessage {
	ret := make([]*specqbft.SignedMessage, 0)
	for round, roundMsgs := range c.messagesByRound {
		for _, msg := range roundMsgs {
			iterator(round, msg)
		}
	}
	return ret
}

// ReadOnlyMessagesByRound returns messagesByRound by the given round
func (c *messagesContainer) ReadOnlyMessagesByRound(round specqbft.Round) []*specqbft.SignedMessage {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return c.messagesByRound[round]
}

func (c *messagesContainer) readOnlyMessagesByRoundAndValue(round specqbft.Round, value []byte) []*specqbft.SignedMessage {
	c.lock.RLock()
	defer c.lock.RUnlock()
	valueHex := hex.EncodeToString(value)

	if _, found := c.messagesByRoundAndValue[round]; !found {
		return nil
	}
	return c.messagesByRoundAndValue[round][valueHex]
}

func (c *messagesContainer) QuorumAchieved(round specqbft.Round, value []byte) (bool, []*specqbft.SignedMessage) {
	if msgs := c.readOnlyMessagesByRoundAndValue(round, value); msgs != nil {
		signers := 0
		retMsgs := make([]*specqbft.SignedMessage, 0)
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
func (c *messagesContainer) AddMessage(msg *specqbft.SignedMessage, data []byte) {
	c.lock.Lock()
	defer c.lock.Unlock()

	valueHex := hex.EncodeToString(data)

	//check msg is not duplicate
	r, err := msg.GetRoot()
	if err != nil {
		return
	}

	for _, existingMsg := range c.messagesByRound[msg.Message.Round] {
		toMatchRoot, err := existingMsg.GetRoot()
		if err != nil {
			return
		}
		if bytes.Equal(r, toMatchRoot) && existingMsg.MatchedSigners(msg.Signers) {
			return
		}
	}

	// add messagesByRound
	_, found := c.messagesByRound[msg.Message.Round]
	if !found {
		c.messagesByRound[msg.Message.Round] = make([]*specqbft.SignedMessage, 0)
	}
	c.messagesByRound[msg.Message.Round] = append(c.messagesByRound[msg.Message.Round], msg)

	// add messages by round and value
	_, found = c.messagesByRoundAndValue[msg.Message.Round]
	if !found {
		c.messagesByRoundAndValue[msg.Message.Round] = make(map[string][]*specqbft.SignedMessage)
	}
	_, found = c.messagesByRoundAndValue[msg.Message.Round][valueHex]
	if !found {
		c.messagesByRoundAndValue[msg.Message.Round][valueHex] = make([]*specqbft.SignedMessage, 0)
	}

	// add to all change round messages
	if msg.Message.MsgType == specqbft.RoundChangeMsgType {
		c.allChangeRoundMessages = append(c.allChangeRoundMessages, msg)
	}

	c.messagesByRoundAndValue[msg.Message.Round][valueHex] = append(c.messagesByRoundAndValue[msg.Message.Round][valueHex], msg)
}

// OverrideMessages will override all current msgs in container with the provided msg
func (c *messagesContainer) OverrideMessages(msg *specqbft.SignedMessage, data []byte) {
	c.lock.Lock()
	// reset previous round data
	delete(c.messagesByRound, msg.Message.Round)
	delete(c.messagesByRoundAndValue, msg.Message.Round)
	c.lock.Unlock()

	// override
	c.AddMessage(msg, data)
}

func (c *messagesContainer) PartialChangeRoundQuorum(stateRound specqbft.Round) (found bool, lowestChangeRound specqbft.Round) {
	lowestChangeRound = specqbft.Round(100000) // just a random really large round number
	foundMsgs := make(map[spectypes.OperatorID]*specqbft.SignedMessage)
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
