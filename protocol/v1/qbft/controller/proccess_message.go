package controller

import (
	"github.com/bloxapp/ssv-spec/ssv"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
)

func (c *Controller) processConsensusMsg(signedMessage *message.SignedMessage) error {
	logger := c.Logger.With(zap.String("type", signedMessage.Message.MsgType.String()),
		zap.Int64("height", int64(signedMessage.Message.Height)),
		zap.Int64("round", int64(signedMessage.Message.Round)),
		zap.Any("sender", signedMessage.GetSigners()))
	if c.ReadMode {
		switch signedMessage.Message.MsgType {
		case message.RoundChangeMsgType, message.CommitMsgType:
		default: // other types not supported in read mode
			return nil
		}
	}
	logger.Debug("process consensus message")
	switch signedMessage.Message.MsgType {
	case message.RoundChangeMsgType: // supporting read-mode
		if c.ReadMode {
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
		if c.GetCurrentInstance() == nil {
			return errors.New("current instance is nil")
		}
		decided, err := c.GetCurrentInstance().ProcessMsg(signedMessage)
		if err != nil {
			return errors.Wrap(err, "failed to process message")
		}
		logger.Debug("current instance processed message", zap.Bool("decided", decided))
	default:
		return errors.Errorf("message type is not suported")
	}
	return nil
}

func (c *Controller) processPostConsensusSig(signedPostConsensusMessage *ssv.SignedPartialSignatureMessage) error {
	return c.ProcessPostConsensusMessage(signedPostConsensusMessage)
}

// processCommitMsg first checks if this msg height is the same as the current instance. if so, need to process as consensus commit msg so no late commit processing.
// if no running instance proceed with the late commit process -
// in case of not "fullSync" and the msg is not the same height as the last decided, late commit will be ignored as there is no other msgs in storage beside the last one.
//
// when there is an updated decided msg -
// and "fullSync" mode, regular process for late commit (saving all range of high msg's)
// if height is the same as last decided msg height, update the last decided with the updated one.
func (c *Controller) processCommitMsg(signedMessage *message.SignedMessage) (bool, error) {
	if c.GetCurrentInstance() != nil {
		if signedMessage.Message.Height >= c.GetCurrentInstance().State().GetHeight() {
			// process as regular consensus commit msg
			return false, nil
		}
	}

	logger := c.Logger.With(zap.String("who", "ProcessLateCommitMsg"),
		zap.Uint64("seq", uint64(signedMessage.Message.Height)),
		zap.String("identifier", signedMessage.Message.Identifier.String()),
		zap.Any("signers", signedMessage.GetSigners()))

	if agg, err := c.ProcessLateCommitMsg(logger, signedMessage); err != nil {
		return false, errors.Wrap(err, "failed to process late commit message")
	} else if agg != nil {
		updated, err := c.DecidedStrategy.UpdateDecided(agg)
		if err != nil {
			return false, errors.Wrap(err, "could not save aggregated decided message")
		}
		if updated != nil {
			logger.Debug("decided message was updated after late commit processing", zap.Any("updated_signers", updated.GetSigners()))
			qbft.ReportDecided(c.ValidatorShare.PublicKey.SerializeToHexStr(), updated)
			if err := c.onNewDecidedMessage(updated); err != nil {
				logger.Error("could not broadcast decided message", zap.Error(err))
			}
		}
	}
	return true, nil
}

// ProcessLateCommitMsg tries to aggregate the late commit message to the corresponding decided message
func (c *Controller) ProcessLateCommitMsg(logger *zap.Logger, msg *message.SignedMessage) (*message.SignedMessage, error) {
	decidedMessages, err := c.DecidedStrategy.GetDecided(msg.Message.Identifier, msg.Message.Height, msg.Message.Height)

	if err != nil {
		return nil, errors.Wrap(err, "could not read decided for late commit")
	}
	if len(decidedMessages) == 0 {
		// decided message does not exist
		logger.Debug("could not find decided")
		return nil, nil
	}
	decidedMsg := decidedMessages[0]
	if msg.Message.Height != decidedMsg.Message.Height { // make sure the same height. if not, pass
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
