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

	cleanContainer := specqbft.NewMsgContainer()
	signerExists := false
	for _, higherMsg := range c.HigherReceivedMessages.AllMessaged() {
		if higherMsg.Message.Height <= c.GetHeight() {
			continue
		}

		if higherMsg.GetSigners()[0] == msg.GetSigners()[0] {
			signerExists = true
		}
		_, err := cleanContainer.AddIfDoesntExist(higherMsg)
		if err != nil {
			return errors.Wrap(err, "could not add higher height msg to clean container")
		}
	}

	if !signerExists {
		_, err := cleanContainer.AddIfDoesntExist(msg)
		if err != nil {
			return errors.Wrap(err, "could not add higher height msg")
		}
	}
	c.HigherReceivedMessages.Msgs = cleanContainer.Msgs
	ok, lowestHeight := c.f1SyncTrigger()
	if ok {
		/*// clean once f+1 achieved TODO spec not supporting cleaning, need to open pr for that
		c.HigherReceivedMessages.Msgs = map[specqbft.Round][]*specqbft.SignedMessage{} // TODO need to add clean func to container
		*/
		knownDecided, err := c.DecidedStrategy.GetLastDecided(c.GetIdentifier())
		if err != nil {
			return errors.Wrap(err, "failed to get known decided")
		}
		logger.Debug("f+1 higher height, trigger decided sync", zap.Int("toHeight", lowestHeight))
		if err := c.syncDecided(knownDecided, nil); err != nil {
			return errors.Wrap(err, "failed to sync decided")
		}
	}
	return nil
}

// f1SyncTrigger returns true if received f+1 higher height messages from unique signers
func (c *Controller) f1SyncTrigger() (bool, int) {
	uniqueSigners := make(map[types.OperatorID]bool)
	lowestHeight := -1
	for _, msg := range c.HigherReceivedMessages.AllMessaged() {
		for _, signer := range msg.GetSigners() {
			if _, found := uniqueSigners[signer]; !found {
				if lowestHeight < 0 || int(msg.Message.Height) < lowestHeight {
					lowestHeight = int(msg.Message.Height)
				}
				uniqueSigners[signer] = true
			}
		}
	}
	return c.ValidatorShare.HasPartialQuorum(len(uniqueSigners)), lowestHeight
}
