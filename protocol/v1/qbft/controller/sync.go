package controller

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/signedmsg"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

func (c *Controller) processHigherHeightMsg(logger *zap.Logger, msg *specqbft.SignedMessage) error {
	if err := pipelines.Combine(
		signedmsg.BasicMsgValidation(),
		signedmsg.ValidateIdentifiers(c.Identifier),
		signedmsg.AuthorizeMsg(c.ValidatorShare)).Run(msg); err != nil {
		return errors.Wrap(err, "invalid msg")
	}

	if !c.verifyAndAddHigherHeightMsg(msg) {
		//return errors.New("discarded future msg") // remove error? need it?
		return nil
	}

	ok := c.f1SyncTrigger()
	if ok {
		knownDecided, err := c.DecidedStrategy.GetLastDecided(c.GetIdentifier())
		if err != nil {
			return errors.Wrap(err, "failed to get known decided")
		}
		logger.Debug("f+1 higher height, trigger decided sync", zap.Any("map", c.HigherReceivedMessages))
		if err := c.syncDecided(knownDecided, nil); err != nil {
			return errors.Wrap(err, "failed to sync decided")
		}
	}
	return nil
}

// verifyAndAddHigherHeightMsg verifies msg, cleanup queue and adds the message if unique signer
func (c *Controller) verifyAndAddHigherHeightMsg(msg *specqbft.SignedMessage) bool {
	cleanedQueue := make(map[types.OperatorID]specqbft.Height)
	signerExists := false
	for signer, height := range c.HigherReceivedMessages {
		if height <= c.GetHeight() {
			continue
		}

		if signer == msg.GetSigners()[0] {
			signerExists = true
		}
		cleanedQueue[signer] = height
	}

	if !signerExists {
		cleanedQueue[msg.GetSigners()[0]] = msg.Message.Height
	}
	c.HigherReceivedMessages = cleanedQueue
	return !signerExists
}

// f1SyncTrigger returns true if received f+1 higher height messages from unique signers
func (c *Controller) f1SyncTrigger() bool {
	return c.ValidatorShare.HasPartialQuorum(len(c.HigherReceivedMessages))
}
