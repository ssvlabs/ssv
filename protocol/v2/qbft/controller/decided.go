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
func (c *Controller) UponDecided(logger *zap.Logger, msg *specqbft.ProcessingMessage) (*spectypes.SignedSSVMessage, error) {
	if err := ValidateDecided(
		c.config,
		msg,
		c.CommitteeMember,
	); err != nil {
		return nil, errors.Wrap(err, "invalid decided msg")
	}

	// try to find instance
	inst := c.StoredInstances.FindInstance(msg.QBFTMessage.Height)

	prevDecided := inst != nil && inst.State.Decided
	isFutureDecided := msg.QBFTMessage.Height > c.Height

	if inst == nil {
		i := instance.NewInstance(c.GetConfig(), c.CommitteeMember, c.Identifier, msg.QBFTMessage.Height, c.OperatorSigner)
		i.State.Round = msg.QBFTMessage.Round
		i.State.Decided = true
		i.State.DecidedValue = msg.SignedMessage.FullData
		err := i.State.CommitContainer.AddMsg(msg)
		if err != nil {
			return nil, err
		}
		c.StoredInstances.addNewInstance(i)
	} else if decided, _ := inst.IsDecided(); !decided {
		inst.State.Decided = true
		inst.State.Round = msg.QBFTMessage.Round
		inst.State.DecidedValue = msg.SignedMessage.FullData
		err := inst.State.CommitContainer.AddMsg(msg)
		if err != nil {
			return nil, err
		}
	} else { // decide previously, add if has more signers
		signers, _ := inst.State.CommitContainer.LongestUniqueSignersForRoundAndRoot(msg.QBFTMessage.Round, msg.QBFTMessage.Root)
		if len(msg.SignedMessage.OperatorIDs) > len(signers) {
			err := inst.State.CommitContainer.AddMsg(msg)
			if err != nil {
				return nil, err
			}
		}
	}

	if isFutureDecided {
		// bump height
		c.Height = msg.QBFTMessage.Height
	}

	if !prevDecided {
		return msg.SignedMessage, nil
	}
	return nil, nil
}

func ValidateDecided(
	config qbft.IConfig,
	msg *specqbft.ProcessingMessage,
	committeeMember *spectypes.CommitteeMember,
) error {
	isDecided, err := IsDecidedMsg(committeeMember, msg)
	if err != nil {
		return err
	}
	if !isDecided {
		return errors.New("not a decided msg")
	}

	if err := instance.BaseCommitValidationVerifySignature(config, msg, msg.QBFTMessage.Height, committeeMember.Committee); err != nil {
		return errors.Wrap(err, "invalid decided msg")
	}

	r, err := specqbft.HashDataRoot(msg.SignedMessage.FullData)
	if err != nil {
		return errors.Wrap(err, "could not hash input data")
	}
	if !bytes.Equal(r[:], msg.QBFTMessage.Root[:]) {
		return errors.New("H(data) != root")
	}

	return nil
}

// IsDecidedMsg returns true if signed commit has all quorum sigs
func IsDecidedMsg(committeeMember *spectypes.CommitteeMember, msg *specqbft.ProcessingMessage) (bool, error) {
	return committeeMember.HasQuorum(len(msg.SignedMessage.OperatorIDs)) && msg.QBFTMessage.MsgType == specqbft.CommitMsgType, nil
}
