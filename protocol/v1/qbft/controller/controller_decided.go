package controller

import (
	"encoding/hex"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
)

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
		MsgID:   c.Identifier,
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

// processDecidedMessage is responsible for processing an incoming decided message.
// we will process decided messages according to the following rules:
// 1. invalid > exit
// 2. old decided > exit
// 3. new message > force decide or stop instance and sync
// 4. last decided, try to update signers
func (c *Controller) processDecidedMessage(msg *specqbft.SignedMessage) error {
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
}

// highestKnownDecided returns the highest known decided instance
func (c *Controller) highestKnownDecided() (*specqbft.SignedMessage, error) {
	highestKnown, err := c.DecidedStrategy.GetLastDecided(message.ToMessageID(c.GetIdentifier()))
	if err != nil {
		return nil, err
	}
	return highestKnown, nil
}

// highestKnownDecided returns the highest known decided instance
func (c *Controller) forceDecided(msg *specqbft.SignedMessage) bool {
	if currentInstance := c.GetCurrentInstance(); currentInstance != nil {
		// check if decided for current instance
		currentState := currentInstance.State()
		if currentState != nil && currentState.GetHeight() == msg.Message.Height {
			currentInstance.ForceDecide(msg)
			return true
		}
	}
	return false
}
