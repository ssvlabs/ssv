package controller

import (
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
)

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
		if currentInstance := c.getCurrentInstance(); currentInstance != nil {
			// if current instance > force decided and exit
			if currentInstance.State() != nil && currentInstance.State().GetHeight() == msg.Message.Height {
				currentInstance.ForceDecide(msg)
				return nil
			}
			logger.Info("stopping current instance and syncing..")
			currentInstance.Stop()
		}
		qbft.ReportDecided(c.ValidatorShare.PublicKey.SerializeToHexStr(), msg)
		if localMsg == nil || msg.Message.Higher(localMsg.Message) {
			return c.syncDecided(localMsg, msg)
		}
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
