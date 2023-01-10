package controller

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v2/qbft"
	"github.com/bloxapp/ssv/protocol/v2/qbft/instance"
)

// UponDecided returns decided msg if decided, nil otherwise
func (c *Controller) UponDecided(msg *specqbft.SignedMessage) (*specqbft.SignedMessage, error) {
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

	inst := c.InstanceForHeight(msg.Message.Height)
	prevDecided := inst != nil && inst.State.Decided
	isFutureDecided := msg.Message.Height > c.Height

	if inst == nil {
		i := instance.NewInstance(c.GetConfig(), c.Share, c.Identifier, msg.Message.Height)
		i.State.Round = msg.Message.Round
		i.State.Decided = true
		i.State.DecidedValue = data.Data
		i.State.CommitContainer.AddMsg(msg)
		c.StoredInstances.addNewInstance(i)
	} else if decided, _ := inst.IsDecided(); !decided {
		inst.State.Decided = true
		inst.State.Round = msg.Message.Round
		inst.State.DecidedValue = data.Data
		inst.State.CommitContainer.AddMsg(msg)
	} else { // decide previously, add if has more signers
		signers, _ := inst.State.CommitContainer.LongestUniqueSignersForRoundAndValue(msg.Message.Round, msg.Message.Data)
		if len(msg.Signers) > len(signers) {
			inst.State.CommitContainer.AddMsg(msg)
		}
	}

	if isFutureDecided {
		// sync gap
		c.GetConfig().GetNetwork().SyncDecidedByRange(spectypes.MessageIDFromBytes(c.Identifier), c.Height, msg.Message.Height)
		// bump height
		c.Height = msg.Message.Height
	}

	if !prevDecided {
		if futureInstance := c.StoredInstances.FindInstance(msg.Message.Height); futureInstance != nil {
			if err = c.SaveHighestInstance(futureInstance, msg); err != nil {
				c.logger.Debug("failed to save instance",
					zap.Uint64("height", uint64(msg.Message.Height)),
					zap.Error(err))
			} else {
				c.logger.Debug("saved instance upon decided", zap.Uint64("height", uint64(msg.Message.Height)))
			}
		}
		return msg, nil
	}
	return nil, nil
}

func validateDecided(
	config qbft.IConfig,
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
