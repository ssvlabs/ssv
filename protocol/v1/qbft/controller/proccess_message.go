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
			return errors.Wrap(err, "failed to process message")
		}
	case message.DecidedMsgType:
		c.ProcessDecidedMessage(signedMessage)
	default:
		return errors.Errorf("message type is not suported")
	}
	return nil
}

func (c *Controller) processPostConsensusSig(signedPostConsensusMessage *message.SignedPostConsensusMessage) error {
	return c.ProcessSignatureMessage(signedPostConsensusMessage)
}
