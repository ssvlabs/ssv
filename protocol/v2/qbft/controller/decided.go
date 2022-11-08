package controller

import (
	qbftspec "github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/protocol/v2/qbft/instance"
	types2 "github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// UponDecided returns decided msg if decided, nil otherwise
func (c *Controller) UponDecided(msg *qbftspec.SignedMessage) (*qbftspec.SignedMessage, error) {
	logger := logex.GetLogger(zap.String("who", "qbftCTRL"), zap.String("where", "UponDecided"),
		zap.Int64("local_height", int64(c.Height)), zap.Int64("msg_height", int64(msg.Message.Height)))
	logger.Debug("upon decided")
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
	if inst := c.InstanceForHeight(c.Height); inst != nil && !inst.State.Decided {
		inst.State.Decided = true
		if c.Height == msg.Message.Height {
			logger.Debug("decided for current instance")
			if msg.Message.Round > inst.State.Round {
				inst.State.Round = msg.Message.Round
			}
			inst.State.DecidedValue = data.Data
		}
	}

	isFutureDecided := msg.Message.Height > c.Height
	if inst == nil {
		logger.Debug("adding instance")
		// add an instance for the decided msg
		i := instance.NewInstance(c.GetConfig(), c.Share, c.Identifier, msg.Message.Height)
		i.State.Round = msg.Message.Round
		i.State.Decided = true
		i.State.DecidedValue = data.Data
		c.StoredInstances.addNewInstance(i)
	}
	if isFutureDecided {
		logger.Debug("future instance, bump height")
		// bump height
		c.Height = msg.Message.Height
	}

	if !prevDecided {
		logger.Debug("instance wasn't previously decided, saving decided message")
		err = c.GetConfig().GetStorage().SaveHighestDecided(msg)
		return msg, errors.Wrap(err, "could not save highest decided")
	}
	return nil, nil
}

func validateDecided(config types2.IConfig, signedDecided *qbftspec.SignedMessage, share *types.Share) error {
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
func isDecidedMsg(share *types.Share, signedDecided *qbftspec.SignedMessage) bool {
	return share.HasQuorum(len(signedDecided.Signers)) && signedDecided.Message.MsgType == qbftspec.CommitMsgType
}
