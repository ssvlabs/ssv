package controller

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"sync/atomic"

	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// ValidateDecidedMsg - the main decided msg pipeline
func (c *Controller) ValidateDecidedMsg(msg *message.SignedMessage) error {
	return c.fork.ValidateDecidedMsg(c.ValidatorShare).Run(msg)
}

// ProcessDecidedMessage is responsible for processing an incoming decided message.
// If the decided message is known or belong to the current executing instance, do nothing.
// Else perform a sync operation
/* From https://arxiv.org/pdf/2002.03613.pdf
We can omit this if we assume some mechanism external to the consensus algorithm that ensures
synchronization of decided values.
upon receiving a valid hROUND-CHANGE, λi, −, −, −i message from pj ∧ pi has decided
by calling Decide(λi,− , Qcommit) do
	send Qcommit to process pj
*/
func (c *Controller) processDecidedMessage(msg *message.SignedMessage) error {
	if err := c.ValidateDecidedMsg(msg); err != nil {
		c.logger.Error("received invalid decided message", zap.Error(err), zap.Any("signer ids", msg.Signers))
		return nil
	}
	logger := c.logger.With(zap.String("who", "processDecided"),
		//zap.Bool("is_full_sync", c.isFullNode()),
		zap.Uint64("height", uint64(msg.Message.Height)),
		zap.Any("signer ids", msg.Signers))
	logger.Debug("received valid decided msg")

	if valid, err := c.strategy.ValidateHeight(msg); err != nil {
		return errors.Wrap(err, "failed to check msg height")
	} else if !valid {
		return nil // msg is too old, do nothing
	}

	// if we already have this in storage, pass
	known, knownMsg, err := c.strategy.IsMsgKnown(msg)
	if err != nil {
		logger.Error("can't check if decided msg is known", zap.Error(err))
		return nil
	}
	if known {
		// if decided is known, check for a more complete message (more signers)
		if ignore := c.checkDecidedMessageSigners(knownMsg, msg); !ignore {
			if err := c.strategy.UpdateDecided(msg); err != nil {
				logger.Error("can't update decided message", zap.Error(err))
				return nil
			}
			logger.Debug("decided was updated")

			qbft.ReportDecided(c.ValidatorShare.PublicKey.SerializeToHexStr(), msg)
			return nil
		}
		logger.Debug("decided is known, skipped")
		return nil
	}

	qbft.ReportDecided(c.ValidatorShare.PublicKey.SerializeToHexStr(), msg)

	if c.readMode {
		if err := c.strategy.SaveDecided(msg); err != nil {
			return errors.Wrap(err, "could not update decided message")
		}
		logger.Debug("decided was updated for controller with read only mode")
		return nil
	}

	// decided for current instance
	if c.forceDecideCurrentInstance(msg) {
		return nil
	}

	// decided for later instances which require a full sync
	shouldSync, err := c.decidedRequiresSync(msg)
	if err != nil {
		logger.Error("can't check decided msg", zap.Error(err))
		return nil
	}
	if shouldSync {
		c.logger.Info("stopping current instance and syncing..")
		if err := c.syncDecided(); err != nil {
			logger.Error("failed sync after decided received", zap.Error(err))
		}
	}
	return nil
}

// forceDecideCurrentInstance will force the current instance to decide provided a signed decided msg.
// will return true if executed, false otherwise
func (c *Controller) forceDecideCurrentInstance(msg *message.SignedMessage) bool {
	if c.decidedForCurrentInstance(msg) {
		// stop current instance
		if c.currentInstance != nil {
			c.currentInstance.ForceDecide(msg)
		}
		return true
	}
	return false
}

// highestKnownDecided returns the highest known decided instance
func (c *Controller) highestKnownDecided() (*message.SignedMessage, error) {
	highestKnown, err := c.ibftStorage.GetLastDecided(c.GetIdentifier())
	if err != nil {
		return nil, err
	}
	return highestKnown, nil
}

// checkDecidedMessageSigners checks if signers of existing decided includes all signers of the newer message
func (c *Controller) checkDecidedMessageSigners(knownMsg *message.SignedMessage, msg *message.SignedMessage) bool {
	// decided message should have at least 3 signers, so if the new decided has 4 signers -> override
	return len(msg.GetSigners()) <= len(knownMsg.GetSigners())
}

// decidedForCurrentInstance returns true if msg has same seq number is current instance
func (c *Controller) decidedForCurrentInstance(msg *message.SignedMessage) bool {
	return c.currentInstance != nil &&
		c.currentInstance.State() != nil &&
		c.currentInstance.State().GetHeight() == msg.Message.Height
}

// decidedRequiresSync returns true if:
// 		- highest known seq lower than msg seq
// 		- AND msg is not for current instance
func (c *Controller) decidedRequiresSync(msg *message.SignedMessage) (bool, error) {
	// if IBFT sync failed to init, trigger it again
	if atomic.LoadUint32(&c.initState) < Ready {
		return true, nil
	}
	if c.decidedForCurrentInstance(msg) {
		return false, nil
	}

	if msg.Message.Height == 0 {
		return false, nil
	}

	highest, err := c.ibftStorage.GetLastDecided(msg.Message.Identifier)
	if highest == nil {
		return msg.Message.Height > 0, nil
	}
	if err != nil {
		return false, errors.Wrap(err, "could not get highest decided instance from storage")
	}
	return highest.Message.Height < msg.Message.Height, nil
}
