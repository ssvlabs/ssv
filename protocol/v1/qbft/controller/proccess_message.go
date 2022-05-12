package controller

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

func (c *Controller) processConsensusMsg(signedMessage *message.SignedMessage) error {
	c.logger.Debug("process consensus message", zap.String("type", signedMessage.Message.MsgType.String()), zap.Int64("height", int64(signedMessage.Message.Height)), zap.Int64("round", int64(signedMessage.Message.Round)), zap.Any("sender", signedMessage.GetSigners()))
	switch signedMessage.Message.MsgType {
	case message.CommitMsgType:
		if processed, err := c.processCommitMsg(signedMessage); err != nil {
			return errors.Wrap(err, "failed to process late commit")
		} else if processed {
			return nil
		}
		fallthrough // not processed, need to process as regular consensus commit msg
	case message.ProposalMsgType, message.PrepareMsgType, message.RoundChangeMsgType:
		if c.currentInstance == nil {
			return errors.New("current instance is nil")
		}
		decided, err := c.currentInstance.ProcessMsg(signedMessage)
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
	c.logger.Debug("process post consensus message", zap.Int64("height", int64(signedPostConsensusMessage.Message.Height)), zap.Int64("signer_id", int64(signedPostConsensusMessage.Message.Signers[0])))
	return c.ProcessSignatureMessage(signedPostConsensusMessage)
}

// processCommitMsg first checks if this msg height is the same as the current instance. if so, need to process as consensus commit msg so no late commit processing.
// if no running instance proceed with the late commit process -
// in case of not "fullSync" and the msg is not the same height as the last decided, late commit will be ignored as there is no other msgs in storage beside the last one.
//
// when there is an updated decided msg -
// and "fullSync" mode, regular process for late commit (saving all range of high msg's)
// if height is the same as last decided msg height, update the last decided with the updated one.
func (c *Controller) processCommitMsg(signedMessage *message.SignedMessage) (bool, error) {
	if c.currentInstance != nil {
		if signedMessage.Message.Height >= c.currentInstance.State().GetHeight() {
			// process as regular consensus commit msg
			return false, nil
		}
	}

	logger := logex.GetLogger(zap.String("who", "ProcessLateCommitMsg"),
		zap.Bool("is_full_sync", c.isFullSync()),
		zap.Uint64("seq", uint64(signedMessage.Message.Height)), zap.String("identifier", string(signedMessage.Message.Identifier)),
		zap.Any("signers", signedMessage.GetSigners()))

	if updated, err := c.ProcessLateCommitMsg(logger, signedMessage); err != nil {
		return false, errors.Wrap(err, "failed to process late commit message")
	} else if updated != nil {

		if c.isFullSync() {
			// process late commit for all range of heights
			if err := c.ibftStorage.SaveDecided(updated); err != nil {
				return false, errors.Wrap(err, "could not save aggregated decided message")
			}
			c.logger.Debug("decided message was updated", zap.Any("updated signers", updated.GetSigners()))
		} else {
			if err := c.ibftStorage.SaveLastDecided(updated); err != nil {
				return false, errors.Wrap(err, "could not save aggregated decided message")
			}
			c.logger.Debug("last decided message was updated", zap.Any("updated signers", updated.GetSigners()))
		}

		qbft.ReportDecided(c.ValidatorShare.PublicKey.SerializeToHexStr(), updated)

		data, err := updated.Encode()
		if err != nil {
			return false, errors.Wrap(err, "failed to encode updated msg")
		}
		ssvMsg := message.SSVMessage{
			MsgType: message.SSVDecidedMsgType,
			ID:      c.Identifier,
			Data:    data,
		}
		if err := c.network.Broadcast(ssvMsg); err != nil {
			c.logger.Error("could not broadcast decided message", zap.Error(err))
		}
		c.logger.Debug("updated decided was broadcasted")
		qbft.ReportDecided(c.ValidatorShare.PublicKey.SerializeToHexStr(), updated)
	}
	return true, nil
}

// ProcessLateCommitMsg tries to aggregate the late commit message to the corresponding decided message
// if not is "fullSync" mode, checks that last decided msg height is the same as the new msg. if not, pass
func (c *Controller) ProcessLateCommitMsg(logger *zap.Logger, msg *message.SignedMessage) (*message.SignedMessage, error) {
	// find stored decided
	var err error
	var decidedMessages []*message.SignedMessage
	if c.isFullSync() {
		decidedMessages, err = c.ibftStorage.GetDecided(msg.Message.Identifier, msg.Message.Height, msg.Message.Height)
	} else {
		var lastDecided *message.SignedMessage
		lastDecided, err = c.ibftStorage.GetLastDecided(msg.Message.Identifier)
		if lastDecided != nil && msg.Message.Height == lastDecided.Message.Height { // make sure the same height. if not, pass
			decidedMessages = append(decidedMessages, lastDecided)
		}
	}

	if err != nil {
		return nil, errors.Wrap(err, "could not read decided for late commit")
	}
	if len(decidedMessages) == 0 {
		// decided message does not exist
		logger.Debug("could not find decided")
		return nil, nil
	}
	decidedMsg := decidedMessages[0]
	if len(decidedMsg.GetSigners()) == c.ValidatorShare.CommitteeSize() {
		// msg was signed by the entire committee
		logger.Debug("msg was signed by the entire committee")
		return nil, nil
	}
	// aggregate message with stored decided
	if err := decidedMsg.Aggregate(msg); err != nil {
		if err == message.ErrDuplicateMsgSigner {
			logger.Debug("duplicated signer")
			return nil, nil
		}
		return nil, errors.Wrap(err, "could not aggregate commit message")
	}
	return decidedMsg, nil
}
