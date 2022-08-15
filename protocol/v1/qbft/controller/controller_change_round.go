package controller

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/message"
)

// ProcessChangeRound check basic pipeline validation than check if height or round is higher than the last one. if so, update
func (c *Controller) ProcessChangeRound(msg *specqbft.SignedMessage) error {
	if err := c.ValidateChangeRoundMsg(msg); err != nil {
		return err
	}
	return UpdateChangeRoundMessage(c.Logger, c.ChangeRoundStorage, msg)
}

// ValidateChangeRoundMsg - validation for read mode change round msg
// validating -
// basic validation, signature, changeRound data
func (c *Controller) ValidateChangeRoundMsg(msg *specqbft.SignedMessage) error {
	return c.Fork.ValidateChangeRoundMsg(c.ValidatorShare, message.ToMessageID(c.Identifier)).Run(msg)
}

// UpdateChangeRoundMessage if round for specific signer is higher than local msg
func UpdateChangeRoundMessage(logger *zap.Logger, changeRoundStorage qbftstorage.ChangeRoundStore, msg *specqbft.SignedMessage) error {
	local, err := changeRoundStorage.GetLastChangeRoundMsg(msg.Message.Identifier, msg.GetSigners()...) // assume 1 signer
	if err != nil {
		return errors.Wrap(err, "failed to get last change round msg")
	}

	fLogger := logger.With(zap.Any("signers", msg.GetSigners()))

	if len(local) == 0 {
		// no last changeRound msg exist, save the first one
		fLogger.Debug("no last change round exist. saving first one", zap.Int64("NewHeight", int64(msg.Message.Height)), zap.Int64("NewRound", int64(msg.Message.Round)))
		return changeRoundStorage.SaveLastChangeRoundMsg(msg)
	}
	lastMsg := local[0]
	fLogger = fLogger.With(
		zap.Int64("lastHeight", int64(lastMsg.Message.Height)),
		zap.Int64("NewHeight", int64(msg.Message.Height)),
		zap.Int64("lastRound", int64(lastMsg.Message.Round)),
		zap.Int64("NewRound", int64(msg.Message.Round)))

	if msg.Message.Height < lastMsg.Message.Height {
		// height is lower than the last known
		fLogger.Debug("new changeRoundMsg height is lower than last changeRoundMsg")
		return nil
	} else if msg.Message.Height == lastMsg.Message.Height {
		if msg.Message.Round <= lastMsg.Message.Round {
			// round is not higher than last known
			fLogger.Debug("new changeRoundMsg round is lower than last changeRoundMsg")
			return nil
		}
	}

	// new msg is higher than last one, save.
	logger.Debug("last change round updated")
	return changeRoundStorage.SaveLastChangeRoundMsg(msg)
}
