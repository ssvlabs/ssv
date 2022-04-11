package controller

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"

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
func (c *Controller) ProcessDecidedMessage(msg *message.SignedMessage) {
	if err := c.ValidateDecidedMsg(msg); err != nil {
		c.logger.Error("received invalid decided message", zap.Error(err), zap.Any("signer ids", msg.Signers))
		return
	}
	logger := c.logger.With(zap.Uint64("seq number", uint64(msg.Message.Height)), zap.Any("signer ids", msg.Signers))

	logger.Debug("received valid decided msg")

	// if we already have this in storage, pass
	known, err := c.decidedMsgKnown(msg)
	if err != nil {
		logger.Error("can't check if decided msg is known", zap.Error(err))
		return
	}
	if known {
		// if decided is known, check for a more complete message (more signers)
		if ignore, _ := c.checkDecidedMessageSigners(msg); !ignore {
			if err := c.ibftStorage.SaveDecided(msg); err != nil {
				logger.Error("can't update decided message", zap.Error(err))
				return
			}
			logger.Debug("decided was updated")
			qbft.ReportDecided(c.ValidatorShare.PublicKey.SerializeToHexStr(), msg)
			return
		}
		logger.Debug("decided is known, skipped")
		return
	}

	qbft.ReportDecided(c.ValidatorShare.PublicKey.SerializeToHexStr(), msg)

	// decided for current instance
	if c.forceDecideCurrentInstance(msg) {
		return
	}

	// decided for later instances which require a full sync
	shouldSync, err := c.decidedRequiresSync(msg)
	if err != nil {
		logger.Error("can't check decided msg", zap.Error(err))
		return
	}
	if shouldSync {
		c.logger.Info("stopping current instance and syncing..")
		if err := c.SyncIBFT(); err != nil {
			logger.Error("failed sync after decided received", zap.Error(err))
		}
	}
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

func (c *Controller) decidedMsgKnown(msg *message.SignedMessage) (bool, error) {
	msgs, err := c.ibftStorage.GetDecided(msg.Message.Identifier, msg.Message.Height, msg.Message.Height)
	if err != nil {
		return false, errors.Wrap(err, "could not get decided instance from storage")
	}
	return len(msgs) > 0, nil
}

// checkDecidedMessageSigners checks if signers of existing decided includes all signers of the newer message
func (c *Controller) checkDecidedMessageSigners(msg *message.SignedMessage) (bool, error) {
	decided, err := c.ibftStorage.GetDecided(msg.Message.Identifier, msg.Message.Height, msg.Message.Height)
	if err != nil {
		return false, errors.Wrap(err, "could not get decided instance from storage")
	}
	if len(decided) == 0 {
		return false, nil
	}
	// decided message should have at least 3 signers, so if the new decided has 4 signers -> override
	if len(decided[0].Signers) < c.ValidatorShare.CommitteeSize() && len(msg.GetSigners()) > len(decided[0].Signers) {
		return false, nil
	}
	return true, nil
}

// decidedForCurrentInstance returns true if msg has same seq number is current instance
func (c *Controller) decidedForCurrentInstance(msg *message.SignedMessage) bool {
	return c.currentInstance != nil && c.currentInstance.State().GetHeight() == msg.Message.Height
}

// decidedRequiresSync returns true if:
// 		- highest known seq lower than msg seq
// 		- AND msg is not for current instance
func (c *Controller) decidedRequiresSync(msg *message.SignedMessage) (bool, error) {
	// if IBFT sync failed to init, trigger it again
	if !c.initSynced.Get() {
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
