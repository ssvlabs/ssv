package controller

import (
	"fmt"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"

	"github.com/bloxapp/ssv/protocol/v2/qbft/instance"
	"github.com/bloxapp/ssv/protocol/v2/types"
)

// UponDecided returns decided msg if decided, nil otherwise
func (c *Controller) UponDecided(msg *specqbft.SignedMessage) (*specqbft.SignedMessage, error) {
	// decided msgs for past (already decided) instances will not decide again, just return
	if msg.Message.Height < c.Height {
		return nil, nil
	}

	if err := validateDecided(
		c.config,
		msg,
		c.Share,
	); err != nil {
		return nil, errors.Wrap(err, "invalid decided msg")
	}

	// get decided value
	data, err := msg.Message.GetCommitData()
	if err != nil {
		return nil, errors.Wrap(err, "could not get decided data")
	}

	// did previously decide?
	inst := c.InstanceForHeight(msg.Message.Height)
	prevDecided := inst != nil && inst.State.Decided

	// Mark current instance decided
	currentInstance := c.InstanceForHeight(c.Height)
	if currentInstance != nil && !currentInstance.State.Decided {
		currentInstance.State.Decided = true
		if c.Height == msg.Message.Height {
			if msg.Message.Round > currentInstance.State.Round {
				currentInstance.State.Round = msg.Message.Round
			}
			currentInstance.State.DecidedValue = data.Data
		}
	}

	isFutureDecided := msg.Message.Height > c.Height
	if isFutureDecided {
		// add an instance for the decided msg
		i := instance.NewInstance(c.GetConfig(), c.Share, c.Identifier, msg.Message.Height)
		i.State.Round = msg.Message.Round
		i.State.Decided = true
		i.State.DecidedValue = data.Data
		c.StoredInstances.addNewInstance(i)
		// bump height
		c.Height = msg.Message.Height
	}

	if !prevDecided {
		if err = c.GetConfig().GetStorage().SaveHighestDecided(msg); err != nil {
			// no need to fail processing the decided msg if failed to save
			fmt.Printf("%s\n", err.Error())
		}
		return msg, nil
	}
	return nil, nil
}

func validateDecided(config types.IConfig,
	signedDecided *specqbft.SignedMessage,
	share *spectypes.Share,
) error {
	if !isDecidedMsg(share, signedDecided) {
		return errors.New("not a decided msg")
	}

	if err := signedDecided.Validate(); err != nil {
		return errors.Wrap(err, "invalid decided msg")
	}

	if err := instance.BaseCommitValidation(config, signedDecided, signedDecided.Message.Height, share.Committee); err != nil {
		return errors.Wrap(err, "invalid decided msg")
	}

	msgDecidedData, err := signedDecided.Message.GetCommitData()
	if err != nil {
		return errors.Wrap(err, "could not get msg decided data")
	}
	if err := msgDecidedData.Validate(); err != nil {
		return errors.Wrap(err, "invalid decided data")
	}

	return nil
}

// isDecidedMsg returns true if signed commit has all quorum sigs
func isDecidedMsg(share *spectypes.Share, signedDecided *specqbft.SignedMessage) bool {
	return share.HasQuorum(len(signedDecided.Signers)) && signedDecided.Message.MsgType == specqbft.CommitMsgType
}
