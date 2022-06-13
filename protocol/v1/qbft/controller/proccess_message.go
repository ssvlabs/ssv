package controller

import (
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/utils/logex"
)

func (c *Controller) processConsensusMsg(signedMessage *message.SignedMessage) error {
	c.logger.Debug("process consensus message", zap.String("type", signedMessage.Message.MsgType.String()), zap.Int64("height", int64(signedMessage.Message.Height)), zap.Int64("round", int64(signedMessage.Message.Round)), zap.Any("sender", signedMessage.GetSigners()))
	if c.readMode {
		if signedMessage.Message.MsgType != message.RoundChangeMsgType {
			return nil // other types not supported in read mode
		}
	}
	switch signedMessage.Message.MsgType {
	case message.RoundChangeMsgType: // supporting read-mode
		if c.readMode {
			return c.ProcessChangeRound(signedMessage)
		}
		fallthrough // not in read mode, need to process regular way
	case message.CommitMsgType:
		if processed, err := c.processCommitMsg(signedMessage); err != nil {
			return errors.Wrap(err, "failed to process late commit")
		} else if processed {
			return nil
		}
		fallthrough // not processed, need to process as regular consensus commit msg
	case message.ProposalMsgType, message.PrepareMsgType:
		if c.getCurrentInstance() == nil {
			return errors.New("current instance is nil")
		}
		decided, err := c.getCurrentInstance().ProcessMsg(signedMessage)
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
	if c.getCurrentInstance() != nil {
		if signedMessage.Message.Height >= c.getCurrentInstance().State().GetHeight() {
			// process as regular consensus commit msg
			return false, nil
		}
	}

	logger := logex.GetLogger(zap.String("who", "ProcessLateCommitMsg"),
		//zap.Bool("is_full_sync", c.isFullNode()),
		zap.Uint64("seq", uint64(signedMessage.Message.Height)),
		zap.String("identifier", signedMessage.Message.Identifier.String()),
		zap.Any("signers", signedMessage.GetSigners()))

	if updated, err := c.ProcessLateCommitMsg(logger, signedMessage); err != nil {
		return false, errors.Wrap(err, "failed to process late commit message")
	} else if updated != nil {
		if err := c.decidedStrategy.SaveLateCommit(updated); err != nil {
			return false, errors.Wrap(err, "could not save aggregated decided message")
		}
		logger.Debug("decided message was updated", zap.Any("updated signers", updated.GetSigners()))

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
			logger.Error("could not broadcast decided message", zap.Error(err))
		}
		logger.Debug("updated decided was broadcasted")
		qbft.ReportDecided(c.ValidatorShare.PublicKey.SerializeToHexStr(), updated)
	}
	return true, nil
}

// ProcessLateCommitMsg tries to aggregate the late commit message to the corresponding decided message
func (c *Controller) ProcessLateCommitMsg(logger *zap.Logger, msg *message.SignedMessage) (*message.SignedMessage, error) {
	decidedMessages, err := c.decidedStrategy.GetDecided(msg.Message.Identifier, msg.Message.Height, msg.Message.Height)

	if err != nil {
		return nil, errors.Wrap(err, "could not read decided for late commit")
	}
	if len(decidedMessages) == 0 {
		// decided message does not exist
		logger.Debug("could not find decided")
		return nil, nil
	}
	decidedMsg := decidedMessages[0]
	if msg.Message.Height == decidedMsg.Message.Height { // make sure the same height. if not, pass
		return nil, nil
	}
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
