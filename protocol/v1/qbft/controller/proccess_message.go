package controller

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

func (c *Controller) processConsensusMsg(signedMessage *message.SignedMessage) error {
	c.logger.Debug("process consensus message", zap.String("type", signedMessage.Message.MsgType.String()), zap.Int64("height", int64(signedMessage.Message.Height)), zap.Int64("round", int64(signedMessage.Message.Round)), zap.Any("sender", signedMessage.GetSigners()))
	switch signedMessage.Message.MsgType {
	case message.ProposalMsgType, message.PrepareMsgType, message.CommitMsgType, message.RoundChangeMsgType:
		if c.currentInstance == nil {
			return errors.New("current instance is nil")
		}
		decided, _, err := c.currentInstance.ProcessMsg(signedMessage)
		if err != nil {
			return errors.Wrap(err, "failed to process message")
		}
		c.logger.Debug("current instance processed message", zap.Bool("decided", decided))
	default:
		return errors.Errorf("message type is not suported")
	}
	return nil
}

func (c *Controller) processPostConsensusSig(signedPostConsensusMessage *message.SignedPostConsensusMessage) error {
	return c.ProcessSignatureMessage(signedPostConsensusMessage)
}
