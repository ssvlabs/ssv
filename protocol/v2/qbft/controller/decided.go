package controller

import (
	"bytes"

	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/protocol/v2/qbft"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
)

// UponDecided returns decided msg if decided, nil otherwise
func (c *Controller) UponDecided(logger *zap.Logger, signedMsg *spectypes.SignedSSVMessage) (*spectypes.SignedSSVMessage, error) {
	if err := ValidateDecided(
		c.config,
		signedMsg,
		c.Share,
	); err != nil {
		return nil, errors.Wrap(err, "invalid decided msg")
	}

	msg, err := specqbft.DecodeMessage(signedMsg.SSVMessage.Data)
	if err != nil {
		return nil, err
	}

	// try to find instance
	inst := c.InstanceForHeight(logger, msg.Height)
	prevDecided := inst != nil && inst.State.Decided
	isFutureDecided := msg.Height > c.Height
	save := true

	if inst == nil {
		i := instance.NewInstance(c.GetConfig(), c.Share, c.Identifier, msg.Height)
		i.State.Round = msg.Round
		i.State.Decided = true
		i.State.DecidedValue = signedMsg.FullData
		err := i.State.CommitContainer.AddMsg(signedMsg)
		if err != nil {
			return nil, err
		}
		c.StoredInstances.addNewInstance(i)
	} else if decided, _ := inst.IsDecided(); !decided {
		inst.State.Decided = true
		inst.State.Round = msg.Round
		inst.State.DecidedValue = signedMsg.FullData
		err := inst.State.CommitContainer.AddMsg(signedMsg)
		if err != nil {
			return nil, err
		}
	} else { // decide previously, add if has more signers
		signers, _ := inst.State.CommitContainer.LongestUniqueSignersForRoundAndRoot(msg.Round, msg.Root)
		if len(signedMsg.GetOperatorIDs()) > len(signers) {
			err := inst.State.CommitContainer.AddMsg(signedMsg)
			if err != nil {
				return nil, err
			}
		} else {
			save = false
		}
	}

	if save {
		// Retrieve instance from StoredInstances (in case it was created above)
		// and save it together with the decided message.
		if inst := c.StoredInstances.FindInstance(msg.Height); inst != nil {
			logger := logger.With(
				zap.Uint64("msg_height", uint64(msg.Height)),
				zap.Uint64("ctrl_height", uint64(c.Height)),
				zap.Any("signers", signedMsg.OperatorIDs),
			)
			if err := c.SaveInstance(inst, signedMsg); err != nil {
				logger.Debug("❗failed to save instance", zap.Error(err))
			} else {
				logger.Debug("💾 saved instance upon decided", zap.Error(err))
			}
		}
	}

	if isFutureDecided {
		// bump height
		c.Height = msg.Height
	}

	if !prevDecided {
		return signedMsg, nil
	}
	return nil, nil
}

func ValidateDecided(
	config qbft.IConfig,
	signedDecided *spectypes.SignedSSVMessage,
	share *spectypes.Operator,
) error {
	isDecided, err := IsDecidedMsg(share, signedDecided)
	if err != nil {
		return err
	}
	if !isDecided {
		return errors.New("not a decided msg")
	}

	if err := signedDecided.Validate(); err != nil {
		return errors.Wrap(err, "invalid decided msg")
	}

	msg, err := specqbft.DecodeMessage(signedDecided.SSVMessage.Data)
	if err != nil {
		return errors.Wrap(err, "can't decode inner SSVMessage")
	}

	if err := instance.BaseCommitValidationVerifySignature(config, signedDecided, msg.Height, share.Committee); err != nil {
		return errors.Wrap(err, "invalid decided msg")
	}

	if err := signedDecided.Validate(); err != nil {
		return errors.Wrap(err, "invalid decided")
	}

	r, err := specqbft.HashDataRoot(signedDecided.FullData)
	if err != nil {
		return errors.Wrap(err, "could not hash input data")
	}
	if !bytes.Equal(r[:], msg.Root[:]) {
		return errors.New("H(data) != root")
	}

	return nil
}

// IsDecidedMsg returns true if signed commit has all quorum sigs
func IsDecidedMsg(share *spectypes.Operator, signedDecided *spectypes.SignedSSVMessage) (bool, error) {

	msg, err := specqbft.DecodeMessage(signedDecided.SSVMessage.Data)
	if err != nil {
		return false, err
	}

	return share.HasQuorum(len(signedDecided.GetOperatorIDs())) && msg.MsgType == specqbft.CommitMsgType, nil
}
