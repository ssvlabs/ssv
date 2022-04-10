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
		// TODO check if instance nil?
		i.currentInstance.ProcessMsg(msg)
	case message.RoundChangeMsgType:
	case message.DecidedMsgType:
		i.ProcessDecidedMessage(msg)
	}

	return false, nil, errors.Errorf("message type is not suported")

	decided, decidedValueBytes, err := controller.ProcessMsg(signedMessage)
	if err != nil {
		return err
	}
	if decided {

	}
	//	 TODO handle decided and decidedValueBytes
	return nil
}

func (c *Controller) processPostConsensusSig(signedPostConsensusMessage *message.SignedPostConsensusMessage) error {
	return c.processSignatureMessage(signedPostConsensusMessage)
}

func (c *Controller) validateMessage(msg *message.SSVMessage) error {
	if !v.share.PublicKey.MessageIDBelongs(msg.GetIdentifier()) {
		return errors.New("msg ID doesn't match validator ID")
	}

	if v.DutyRunners.DutyRunnerForMsgID(msg.GetIdentifier()) == nil {
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
