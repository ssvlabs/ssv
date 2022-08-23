package controller

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance"
	"go.uber.org/zap"
)

// ProcessChangeRound check basic pipeline validation than check if height or round is higher than the last one. if so, update
func (c *Controller) ProcessChangeRound(msg *specqbft.SignedMessage) error {
	if err := c.ValidateChangeRoundMsg(msg); err != nil {
		return err
	}
	return instance.UpdateChangeRoundMessage(c.Logger, c.ChangeRoundStorage, msg)
}

// ValidateChangeRoundMsg - validation for read mode change round msg
// validating -
// basic validation, signature, changeRound data
func (c *Controller) ValidateChangeRoundMsg(msg *specqbft.SignedMessage) error {
	return c.Fork.ValidateChangeRoundMsg(c.ValidatorShare, message.ToMessageID(c.Identifier)).Run(msg)
}

func (c *Controller) loadLastChangeRound() {
	var singers []spectypes.OperatorID
	for k := range c.ValidatorShare.Committee { // get all possible msg's from committee
		singers = append(singers, k)
	}

	msgs, err := c.ChangeRoundStorage.GetLastChangeRoundMsg(c.Identifier, singers...)
	if err != nil {
		c.Logger.Warn("failed to load change round messages from storage", zap.Error(err))
		return
	}

	res := make(map[spectypes.OperatorID]specqbft.Round)
	for _, msg := range msgs {
		encoded, err := msg.Encode()
		if err != nil {
			continue
		}
		c.Q.Add(&spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   message.ToMessageID(msg.Message.Identifier),
			Data:    encoded,
		})
		res[msg.GetSigners()[0]] = msg.Message.Round // assuming 1 signer in change round msg
	}
	c.Logger.Info("successfully loaded change round messages from storage into queue", zap.Any("msgs", res))
}
