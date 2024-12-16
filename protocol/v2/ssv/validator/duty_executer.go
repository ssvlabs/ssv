package validator

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

func (v *Validator) OnExecuteDuty(ctx context.Context, logger *zap.Logger, msg *types.EventMsg) error {
	executeDutyData, err := msg.GetExecuteDutyData()
	if err != nil {
		return fmt.Errorf("failed to get execute duty data: %w", err)
	}

	logger = logger.With(fields.Slot(executeDutyData.Duty.DutySlot()), fields.Role(executeDutyData.Duty.RunnerRole()))

	// force the validator to be started (subscribed to validator's topic and synced)
	if _, err := v.Start(logger); err != nil {
		return fmt.Errorf("could not start validator: %w", err)
	}
	if err := v.StartDuty(ctx, logger, executeDutyData.Duty); err != nil {
		return fmt.Errorf("could not start duty: %w", err)
	}

	return nil
}

func (c *Committee) OnExecuteDuty(ctx context.Context, logger *zap.Logger, msg *types.EventMsg) error {
	executeDutyData, err := msg.GetExecuteCommitteeDutyData()
	if err != nil {
		return fmt.Errorf("failed to get execute committee duty data: %w", err)
	}

	if err := c.StartDuty(ctx, logger, executeDutyData.Duty); err != nil {
		return fmt.Errorf("could not start committee duty: %w", err)
	}

	if err := c.StartConsumeQueue(logger, executeDutyData.Duty); err != nil {
		return fmt.Errorf("could not start committee consume queue: %w", err)
	}

	return nil
}
