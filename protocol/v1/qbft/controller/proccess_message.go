package controller

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/pkg/errors"
)

func (c *Controller) processConsensusMsg(signedMessage *message.SignedMessage) error {
	switch signedMessage.Message.MsgType {
	case message.ProposalMsgType:
	case message.PrepareMsgType:
	case message.CommitMsgType:
	case message.RoundChangeMsgType:
		// TODO check if instance nil?
		_, _, err := c.currentInstance.ProcessMsg(signedMessage)
		if err != nil {
			return err
		}
	//
	//case message.DecidedMsgType:
	//	c.ProcessDecidedMessage(signedMessage)
	default:
		return errors.Errorf("message type is not suported")
	}

}

func (c *Controller) processPostConsensusSig(signedPostConsensusMessage *message.SignedPostConsensusMessage) error {
	return c.processSignatureMessage(signedPostConsensusMessage)
}

func (c *Controller) validateMessage(msg *message.SSVMessage) error {
	if !c.ValidatorShare.PublicKey.MessageIDBelongs(msg.GetIdentifier()) {
		return errors.New("msg ID doesn't match validator ID")
	}

	if c.DutyRunners.DutyRunnerForMsgID(msg.GetIdentifier()) == nil {
		return errors.New("could not find duty runner for msg ID")
	}

	if msg.GetType() > 2 {
		return errors.New("msg type not supported")
	}

	if len(msg.GetData()) == 0 {
		return errors.New("msg data is invalid")
	}

	return nil
}
