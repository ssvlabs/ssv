package controller

import (
	"encoding/hex"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/signedmsg"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
)

func (c *Controller) uponFutureDecided(logger *zap.Logger, msg *specqbft.SignedMessage) (bool, error) {
	if err := c.validateDecided(msg); err != nil {
		return false, errors.Wrap(err, "invalid decided msg")
	}

	heighr := msg.Message.Height >= c.GetHeight()
	// 1, when more than 1 instance implemented need to create new instance and add it (after fully ssv spec alignment)
	// 2, save decided
	updated, err := c.DecidedStrategy.UpdateDecided(msg)
	if err != nil {
		return false, err
	}
	if heighr && updated != nil { // only notify when higher or equal height updated
		qbft.ReportDecided(hex.EncodeToString(message.ToMessageID(msg.Message.Identifier).GetPubKey()), updated)
		if c.newDecidedHandler != nil {
			go c.newDecidedHandler(msg)
		}
	}
	// 3, close instance if exist
	if currentInstance := c.GetCurrentInstance(); currentInstance != nil {
		currentInstance.Stop()
		logger.Debug("future decided received. closing instance")
	}
	// 4, set ctrl height as the new decided
	if heighr {
		c.setHeight(msg.Message.Height)
		logger.Debug("higher decided has been updated")
		return true, nil
	}
	logger.Debug("late decided has been updated")
	return false, nil // TODO need to return "decided" false in that case?
}

// onNewDecidedMessage handles a new decided message, will be called at max twice in an epoch for a single validator.
// in read mode, we don't broadcast the message in the network
func (c *Controller) onNewDecidedMessage(msg *specqbft.SignedMessage) error {
	qbft.ReportDecided(hex.EncodeToString(message.ToMessageID(msg.Message.Identifier).GetPubKey()), msg)
	// encode the message first to avoid sharing msg with 2 goroutines
	data, err := msg.Encode()
	if err != nil {
		return errors.Wrap(err, "failed to encode updated msg")
	}
	if c.newDecidedHandler != nil {
		go c.newDecidedHandler(msg)
	}
	if c.ReadMode {
		return nil
	}

	if err := c.Network.Broadcast(spectypes.SSVMessage{
		MsgType: spectypes.SSVDecidedMsgType,
		MsgID:   message.ToMessageID(c.Identifier),
		Data:    data,
	}); err != nil {
		return errors.Wrap(err, "could not broadcast decided message")
	}
	return nil
}

// ValidateDecidedMsg - the main decided msg pipeline
func (c *Controller) ValidateDecidedMsg(msg *specqbft.SignedMessage) error {
	return c.Fork.ValidateDecidedMsg(c.ValidatorShare).Run(msg)
}

func (c *Controller) validateDecided(msg *specqbft.SignedMessage) error {
	return pipelines.Combine(
		signedmsg.BasicMsgValidation(),
		signedmsg.ValidateIdentifiers(c.Identifier),
		signedmsg.MsgTypeCheck(specqbft.CommitMsgType),
		pipelines.WrapFunc("commit data validation", func(signedMessage *specqbft.SignedMessage) error {
			msgCommitData, err := msg.Message.GetCommitData()
			if err != nil {
				return errors.Wrap(err, "could not get msg commit data")
			}
			if err := msgCommitData.Validate(); err != nil {
				return errors.Wrap(err, "msgCommitData invalid")
			}
			return nil
		}), // TODO add as permanent func
		signedmsg.ValidateQuorum(c.ValidatorShare.ThresholdSize()),
		signedmsg.AuthorizeMsg(c.ValidatorShare)).Run(msg)
}

// processDecidedMessage is responsible for processing an incoming decided message.
// we will process decided messages according to the following rules:
// 1. invalid > exit
// 2. old decided > exit
// 3. new message > force decide or stop instance and sync
// 4. last decided, try to update signers
// TODO need to be removed
/* func (c *Controller) processDecidedMessage(msg *specqbft.SignedMessage) error {
	if err := c.ValidateDecidedMsg(msg); err != nil {
		c.Logger.Error("received invalid decided message", zap.Error(err), zap.Any("signer ids", msg.Signers))
		return nil
	}
	logger := c.Logger.With(zap.String("who", "processDecided"),
		zap.Uint64("height", uint64(msg.Message.Height)),
		zap.Any("signer ids", msg.Signers))
	logger.Debug("received valid decided msg")

	localMsg, err := c.highestKnownDecided()
	if err != nil {
		logger.Warn("could not read local decided message", zap.Error(err))
		return err
	}
	// old message
	if localMsg != nil && localMsg.Message.Height > msg.Message.Height {
		logger.Debug("known decided msg")
		return nil
	}
	// new message, force decide or stop instance and sync
	if localMsg == nil || msg.Message.Height > localMsg.Message.Height {
		if c.forceDecided(msg) {
			logger.Debug("current instance decided")
			return nil
		}
		updated, err := c.DecidedStrategy.UpdateDecided(msg)
		if err != nil {
			return err
		}
		if updated != nil {
			qbft.ReportDecided(hex.EncodeToString(message.ToMessageID(msg.Message.Identifier).GetPubKey()), updated)
			if c.newDecidedHandler != nil {
				go c.newDecidedHandler(msg)
			}
		}
		if currentInstance := c.GetCurrentInstance(); currentInstance != nil {
			logger.Debug("stopping current instance")
			currentInstance.Stop()
		}
		return c.syncDecided(localMsg, msg)
	}
	// last decided, try to update it (merge new signers)
	if updated, err := c.DecidedStrategy.UpdateDecided(msg); err != nil {
		logger.Warn("could not update decided")
	} else if updated != nil {
		qbft.ReportDecided(hex.EncodeToString(message.ToMessageID(msg.Message.Identifier).GetPubKey()), updated)
		if c.newDecidedHandler != nil {
			go c.newDecidedHandler(msg)
		}
	}
	return err
	return nil
}*/

// highestKnownDecided returns the highest known decided instance
func (c *Controller) highestKnownDecided() (*specqbft.SignedMessage, error) {
	highestKnown, err := c.DecidedStrategy.GetLastDecided(c.GetIdentifier())
	if err != nil {
		return nil, err
	}
	return highestKnown, nil
}

// returns true if signed commit has all quorum sigs
func (c *Controller) isDecidedMsg(signedDecided *specqbft.SignedMessage) bool {
	return c.ValidatorShare.HasQuorum(len(signedDecided.Signers)) && signedDecided.Message.MsgType == specqbft.CommitMsgType
}
