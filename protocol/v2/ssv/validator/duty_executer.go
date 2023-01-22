package validator

import (
	"encoding/hex"
	"github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

func (v *Validator) OnExecuteDuty(msg types.EventMsg) error {
	executeDutyData, err := msg.GetExecuteDutyData()
	if err != nil {
		return errors.Wrap(err, "failed to get execute duty data")
	}
	// force the validator to be started (subscribed to validator's topic and synced)
	if err := v.Start(); err != nil {
		return errors.Wrap(err, "could not start validator")
	}
	logger.Info("starting duty processing", zap.Any("slot", executeDutyData.Duty.Slot),
		zap.String("type", executeDutyData.Duty.Type.String()), zap.String("publicKey", hex.EncodeToString(executeDutyData.Duty.PubKey[:])))
	if err := v.StartDuty(executeDutyData.Duty); err != nil {
		return errors.Wrap(err, "could not start duty")
	}
	return nil
}
