package controller

import (
	"bytes"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v2/qbft"
	"github.com/bloxapp/ssv/protocol/v2/qbft/instance"
)

// UponDecided returns decided msg if decided, nil otherwise
func (c *Controller) UponDecided(logger *zap.Logger, msg *specqbft.SignedMessage) (*specqbft.SignedMessage, error) {
	if err := ValidateDecided(
		c.config,
		msg,
		c.Share,
	); err != nil {
		return nil, errors.Wrap(err, "invalid decided msg")
	}

	// try to find instance
	inst := c.InstanceForHeight(logger, msg.Message.Height)
	prevDecided := inst != nil && inst.State.Decided
	isFutureDecided := msg.Message.Height > c.Height
	save := true

	if inst == nil {
		i := instance.NewInstance(c.GetConfig(), c.Share, c.Identifier, msg.Message.Height)
		i.State.Round = msg.Message.Round
		i.State.Decided = true
		i.State.DecidedValue = msg.FullData
		i.State.CommitContainer.AddMsg(msg)
		c.StoredInstances.addNewInstance(i)
	} else if decided, _ := inst.IsDecided(); !decided {
		inst.State.Decided = true
		inst.State.Round = msg.Message.Round
		inst.State.DecidedValue = msg.FullData
		inst.State.CommitContainer.AddMsg(msg)
	} else { // decide previously, add if has more signers
		signers, _ := inst.State.CommitContainer.LongestUniqueSignersForRoundAndRoot(msg.Message.Round, msg.Message.Root)
		if len(msg.Signers) > len(signers) {
			inst.State.CommitContainer.AddMsg(msg)
		} else {
			save = false
		}
	}

	if save {
		// Retrieve instance from StoredInstances (in case it was created above)
		// and save it together with the decided message.
		if inst := c.StoredInstances.FindInstance(msg.Message.Height); inst != nil {
			logger := logger.With(
				zap.Uint64("msg_height", uint64(msg.Message.Height)),
				zap.Uint64("ctrl_height", uint64(c.Height)),
				zap.Any("signers", msg.Signers),
			)
			if err := c.SaveInstance(inst, msg); err != nil {
				logger.Debug("❗failed to save instance", zap.Error(err))
			} else {
				logger.Debug("💾 saved instance upon decided", zap.Error(err))
			}
		}
	}

	if isFutureDecided {
		// bump height
		c.Height = msg.Message.Height
	}
	if c.NewDecidedHandler != nil {
		c.NewDecidedHandler(msg)
	}
	if !prevDecided {
		return msg, nil
	}
	return nil, nil
}

func ValidateDecided(
	config qbft.IConfig,
	signedDecided *specqbft.SignedMessage,
	share *spectypes.Share,
) error {
	if !IsDecidedMsg(share, signedDecided) {
		return errors.New("not a decided msg")
	}

	if err := signedDecided.Validate(); err != nil {
		return errors.Wrap(err, "invalid decided msg")
	}

	if err := instance.BaseCommitValidation(config, signedDecided, signedDecided.Message.Height, share.Committee); err != nil {
		return errors.Wrap(err, "invalid decided msg")
	}

	if err := signedDecided.Validate(); err != nil {
		return errors.Wrap(err, "invalid decided")
	}

	r, err := specqbft.HashDataRoot(signedDecided.FullData)
	if err != nil {
		return errors.Wrap(err, "could not hash input data")
	}
	if !bytes.Equal(r[:], signedDecided.Message.Root[:]) {
		return errors.New("H(data) != root")
	}

	return nil
}

// IsDecidedMsg returns true if signed commit has all quorum sigs
func IsDecidedMsg(share *spectypes.Share, signedDecided *specqbft.SignedMessage) bool {
	return share.HasQuorum(len(signedDecided.Signers)) && signedDecided.Message.MsgType == specqbft.CommitMsgType
}
