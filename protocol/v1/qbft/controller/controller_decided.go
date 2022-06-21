package controller

import (
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
)

// onNewDecidedMessage handles a new decided message, will be called at max twice in an epoch for a single validator.
// in read mode, we don't broadcast the message in the network
func (c *Controller) onNewDecidedMessage(msg *message.SignedMessage) error {
	// encode the message first to avoid sharing msg with 2 goroutines
	data, err := msg.Encode()
	if err != nil {
		return errors.Wrap(err, "failed to encode updated msg")
	}
	if c.newDecidedHandler != nil {
		go c.newDecidedHandler(msg)
	}
	if c.readMode {
		return nil
	}
	if err := c.network.Broadcast(message.SSVMessage{
		MsgType: message.SSVDecidedMsgType,
		ID:      c.Identifier,
		Data:    data,
	}); err != nil {
		return errors.Wrap(err, "could not broadcast decided message")
	}
	return nil
}

// ValidateDecidedMsg - the main decided msg pipeline
func (c *Controller) ValidateDecidedMsg(msg *message.SignedMessage) error {
	return c.fork.ValidateDecidedMsg(c.ValidatorShare).Run(msg)
}

// processDecidedMessage is responsible for processing an incoming decided message.
func (c *Controller) processDecidedMessage(msg *message.SignedMessage) error {
	if err := c.ValidateDecidedMsg(msg); err != nil {
		c.logger.Error("received invalid decided message", zap.Error(err), zap.Any("signer ids", msg.Signers))
		return nil
	}
	logger := c.logger.With(zap.String("who", "processDecided"),
		zap.Uint64("height", uint64(msg.Message.Height)),
		zap.Any("signer ids", msg.Signers))
	logger.Debug("received valid decided msg")

	localMsg, err := c.highestKnownDecided()
	if err != nil {
		logger.Warn("could not read local decided message", zap.Error(err))
		return err
	}
	// if local msg is not higher, force decided or stop instance + sync for newer messages
	if localMsg == nil || !localMsg.Message.Higher(msg.Message) {
		updated, err := c.decidedStrategy.UpdateDecided(msg)
		if err != nil {
			return err
		}
		if updated != nil && c.readMode && c.newDecidedHandler != nil {
			go c.newDecidedHandler(updated)
		}
		if currentInstance := c.getCurrentInstance(); currentInstance != nil {
			// check if decided for current instance
			currentState := currentInstance.State()
			if currentState != nil && currentState.GetHeight() == msg.Message.Height {
				logger.Debug("current instance decided")
				currentInstance.ForceDecide(msg)
				return nil
			}
			if updated != nil {
				logger.Debug("stopping current instance")
				currentInstance.Stop()
			}
		}
		qbft.ReportDecided(c.ValidatorShare.PublicKey.SerializeToHexStr(), msg)
		if localMsg == nil || msg.Message.Higher(localMsg.Message) {
			logger.Debug("syncing")
			return c.syncDecided(localMsg, msg)
		}
	} else {
		logger.Debug("known decided msg")
	}

	return err
}

// highestKnownDecided returns the highest known decided instance
func (c *Controller) highestKnownDecided() (*message.SignedMessage, error) {
	highestKnown, err := c.decidedStrategy.GetLastDecided(c.GetIdentifier())
	if err != nil {
		return nil, err
	}
	return highestKnown, nil
}
