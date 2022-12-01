package controller

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"

	"github.com/bloxapp/ssv/protocol/v2/types"
)

func (c *Controller) UponFutureMsg(msg *specqbft.SignedMessage) (*specqbft.SignedMessage, error) {
	c.logger.Debug("UponFutureMsg")
	if err := validateFutureMsg(c.GetConfig(), msg, c.Share.Committee); err != nil {
		return nil, errors.Wrap(err, "invalid future msg")
	}
	if !c.addHigherHeightMsg(msg) {
		return nil, errors.New("discarded future msg")
	}
	if c.f1SyncTrigger() {
		return nil, c.GetConfig().GetNetwork().SyncHighestDecided(spectypes.MessageIDFromBytes(c.Identifier))
	}
	return nil, nil
}

func validateFutureMsg(
	config types.IConfig,
	msg *specqbft.SignedMessage,
	operators []*spectypes.Operator,
) error {
	if err := msg.Validate(); err != nil {
		return errors.Wrap(err, "invalid decided msg")
	}

	if len(msg.GetSigners()) != 1 {
		return errors.New("allows 1 signer")
	}

	// verify signature
	if err := msg.Signature.VerifyByOperators(msg, config.GetSignatureDomainType(), spectypes.QBFTSignatureType, operators); err != nil {
		return errors.Wrap(err, "commit msg signature invalid")
	}

	return nil
}

// addHigherHeightMsg verifies msg, cleanup queue and adds the message if unique signer
func (c *Controller) addHigherHeightMsg(msg *specqbft.SignedMessage) bool {
	// cleanup lower height msgs
	cleanedQueue := make(map[spectypes.OperatorID]specqbft.Height)
	signerExists := false
	for signer, height := range c.FutureMsgsContainer {
		if height <= c.Height {
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
	c.FutureMsgsContainer = cleanedQueue
	return !signerExists
}

// f1SyncTrigger returns true if received f+1 higher height messages from unique signers
func (c *Controller) f1SyncTrigger() bool {
	return c.Share.HasPartialQuorum(len(c.FutureMsgsContainer))
}
