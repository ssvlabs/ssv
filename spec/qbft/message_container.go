package qbft

import (
	"bytes"
	"encoding/json"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/pkg/errors"
)

type MsgContainer struct {
	Msgs map[Round][]*SignedMessage
}

func NewMsgContainer() *MsgContainer {
	return &MsgContainer{
		Msgs: map[Round][]*SignedMessage{},
	}
}

// AllMessaged returns all messages
func (c *MsgContainer) AllMessaged() []*SignedMessage {
	ret := make([]*SignedMessage, 0)
	for _, roundMsgs := range c.Msgs {
		for _, msg := range roundMsgs {
			ret = append(ret, msg)
		}
	}
	return ret
}

// MessagesForRound returns all msgs for Height and round, empty slice otherwise
func (c *MsgContainer) MessagesForRound(round Round) []*SignedMessage {
	if c.Msgs[round] != nil {
		return c.Msgs[round]
	}
	return make([]*SignedMessage, 0)
}

// UniqueSignersSetForRoundAndValue returns the longest set of unique signers and msgs for a specific round and value
func (c *MsgContainer) UniqueSignersSetForRoundAndValue(round Round, value []byte) ([]types.OperatorID, []*SignedMessage) {
	signersRet := make([]types.OperatorID, 0)
	msgsRet := make([]*SignedMessage, 0)
	if c.Msgs[round] == nil {
		return signersRet, msgsRet
	}

	for i := 0; i < len(c.Msgs[round]); i++ {
		m := c.Msgs[round][i]

		if !bytes.Equal(m.Message.Data, value) {
			continue
		}

		currentSigners := make([]types.OperatorID, 0)
		currentMsgs := make([]*SignedMessage, 0)
		currentMsgs = append(currentMsgs, m)
		currentSigners = append(currentSigners, m.GetSigners()...)
		for j := i + 1; j < len(c.Msgs[round]); j++ {
			m2 := c.Msgs[round][j]

			if !bytes.Equal(m2.Message.Data, value) {
				continue
			}

			if !m2.CommonSigners(currentSigners) {
				currentMsgs = append(currentMsgs, m2)
				currentSigners = append(currentSigners, m2.GetSigners()...)
			}
		}

		if len(signersRet) < len(currentSigners) {
			signersRet = currentSigners
			msgsRet = currentMsgs
		}
	}

	return signersRet, msgsRet
}

// AddIfDoesntExist will add a msg only if there isn't an existing msg with the same root and signers
func (c *MsgContainer) AddIfDoesntExist(msg *SignedMessage) (bool, error) {
	if c.Msgs[msg.Message.Round] == nil {
		c.Msgs[msg.Message.Round] = make([]*SignedMessage, 0)
	}

	r, err := msg.GetRoot()
	if err != nil {
		return false, errors.Wrap(err, "could not get signed msg root")
	}

	for _, existingMsg := range c.Msgs[msg.Message.Round] {
		toMatchRoot, err := existingMsg.GetRoot()
		if err != nil {
			return false, errors.Wrap(err, "could not get existing signed msg root")
		}
		if bytes.Equal(r, toMatchRoot) && existingMsg.MatchedSigners(msg.Signers) {
			return false, nil
		}
	}

	// add msg
	c.Msgs[msg.Message.Round] = append(c.Msgs[msg.Message.Round], msg)
	return true, nil
}

// Encode returns the encoded struct in bytes or error
func (c *MsgContainer) Encode() ([]byte, error) {
	return json.Marshal(c)
}

// Decode returns error if decoding failed
func (c *MsgContainer) Decode(data []byte) error {
	return json.Unmarshal(data, &c)
}
