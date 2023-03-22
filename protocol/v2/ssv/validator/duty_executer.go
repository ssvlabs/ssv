package validator

import (
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v2/types"
)

func (v *Validator) OnExecuteDuty(logger *zap.Logger, msg types.EventMsg) error {
	executeDutyData, err := msg.GetExecuteDutyData()
	if err != nil {
		return errors.Wrap(err, "failed to get execute duty data")
	}

	// force the validator to be started (subscribed to validator's topic and synced)
	if err := v.Start(logger); err != nil {
		return errors.Wrap(err, "could not start validator")
	}

	logger.Info("ℹ️ starting duty processing", zap.Any("slot", executeDutyData.Duty.Slot),
		zap.String("type", executeDutyData.Duty.Type.String()))

	if err := v.StartDuty(logger, executeDutyData.Duty); err != nil {
		return errors.Wrap(err, "could not start duty")
	}

	return nil
}
