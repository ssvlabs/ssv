package validator

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/protocol/v2/qbft/instance"
	"github.com/bloxapp/ssv/protocol/v2/qbft/roundtimer"
	"github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

func (v *Validator) registerTimeoutHandler(instance *instance.Instance, height specqbft.Height) {
	identifier := spectypes.MessageIDFromBytes(instance.State.ID)
	timer, ok := instance.GetConfig().GetTimer().(*roundtimer.RoundTimer)
	if ok {
		timer.OnTimeout(v.onTimeout(identifier, height))
	}
}

func (v *Validator) onTimeout(identifier spectypes.MessageID, height specqbft.Height) func() {
	return func() {
		dr := v.DutyRunners[identifier.GetRoleType()]
		if !dr.HasRunningDuty() {
			return
		}

		msg, err := v.createTimerMessage(identifier, height)
		if err != nil {
			v.logger.Warn("failed to create timer msg", zap.Error(err))
			return
		}
		v.logger.Debug("NIV: onTimeout. adding msg to queue", zap.String("role", identifier.GetRoleType().String()), zap.Int64("h", int64(height)))
		v.Queues[identifier.GetRoleType()].Add(msg)
	}
}

func (v *Validator) createTimerMessage(identifier spectypes.MessageID, height specqbft.Height) (*spectypes.SSVMessage, error) {
	msg := &specqbft.Message{
		MsgType:    types.Timer,
		Height:     height,
		Round:      0, // needed?
		Identifier: identifier[:],
		Data:       nil,
	}
	sig, err := v.Signer.SignRoot(msg, spectypes.QBFTSignatureType, v.Share.SharePubKey)
	if err != nil {
		return nil, errors.Wrap(err, "failed signing timer msg")
	}

	sm := &specqbft.SignedMessage{
		Signature: sig,
		Signers:   []spectypes.OperatorID{v.Share.OperatorID},
		Message:   msg,
	}

	data, err := sm.Encode()
	if err != nil {
		return nil, errors.Wrap(err, "failed to encode timeout signed msg")
	}
	return &spectypes.SSVMessage{
		MsgType: types.SSVTimerMsgType,
		MsgID:   identifier,
		Data:    data,
	}, nil
}
