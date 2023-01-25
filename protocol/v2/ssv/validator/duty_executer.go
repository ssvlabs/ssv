package validator

import (
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
	v.logger.Info("starting duty processing", zap.Any("slot", executeDutyData.Duty.Slot),
		zap.String("type", executeDutyData.Duty.Type.String()))
	if err := v.StartDuty(executeDutyData.Duty); err != nil {
		return errors.Wrap(err, "could not start duty")
	}
	return nil
}
