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
	added, err := c.HigherReceivedMessages.AddIfDoesntExist(msg)
	if err != nil {
		return errors.Wrap(err, "could not add higher height msg")
	}
	ok, lowestHeight := c.f1SyncTrigger()
	if added && ok {
		// TODO should reset msg container? past msgs? all msgs?
		// clean once f+1 achieved
		c.HigherReceivedMessages.Msgs = map[specqbft.Round][]*specqbft.SignedMessage{} // TODO need to add clean func to container

		logger.Debug("f+1 higher height, trigger decided sync", zap.Int("toHeight", lowestHeight))
		if err := c.syncDecided(msg, nil); err != nil {
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
