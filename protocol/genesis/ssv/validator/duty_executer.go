package validator

import (
	"github.com/pkg/errors"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/genesis/types"
)

func (v *Validator) OnExecuteDuty(logger *zap.Logger, msg types.EventMsg) error {
	executeDutyData, err := msg.GetExecuteDutyData()
	if err != nil {
		return errors.Wrap(err, "failed to get execute duty data")
	}

	logger = logger.With(fields.Slot(executeDutyData.Duty.Slot), fields.BeaconRole(spectypes.BeaconRole(executeDutyData.Duty.Type)))

	// force the validator to be started (subscribed to validator's topic and synced)
	if _, err := v.Start(logger); err != nil {
		return errors.Wrap(err, "could not start validator")
	}
	if err := v.StartDuty(logger, executeDutyData.Duty); err != nil {
		return errors.Wrap(err, "could not start duty")
	}

	return nil
}
