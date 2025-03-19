package validator

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

func (v *Validator) OnExecuteDuty(ctx context.Context, logger *zap.Logger, msg *types.EventMsg) error {
	ctx, span := tracer.Start(ctx,
		fmt.Sprintf("%s.on_execute_duty", observabilityNamespace),
		trace.WithAttributes(
			observability.ValidatorEventTypeAttribute(msg.Type),
		))
	defer span.End()

	executeDutyData, err := msg.GetExecuteDutyData()
	if err != nil {
		err := fmt.Errorf("failed to get execute duty data: %w", err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	span.SetAttributes(
		observability.BeaconSlotAttribute(executeDutyData.Duty.Slot),
		observability.RunnerRoleAttribute(executeDutyData.Duty.RunnerRole()),
	)
	logger = logger.With(fields.Slot(executeDutyData.Duty.DutySlot()), fields.Role(executeDutyData.Duty.RunnerRole()))

	// force the validator to be started (subscribed to validator's topic and synced)
	span.AddEvent("start validator")
	if _, err := v.Start(logger); err != nil {
		err := fmt.Errorf("could not start validator: %w", err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	span.AddEvent("start duty")
	if err := v.StartDuty(ctx, logger, executeDutyData.Duty); err != nil {
		err := fmt.Errorf("could not start duty: %w", err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	span.SetStatus(codes.Ok, "")
	return nil
}

func (c *Committee) OnExecuteDuty(ctx context.Context, logger *zap.Logger, msg *types.EventMsg) error {
	ctx, span := tracer.Start(ctx,
		fmt.Sprintf("%s.on_execute_committee_duty", observabilityNamespace),
		trace.WithAttributes(
			observability.ValidatorEventTypeAttribute(msg.Type),
		))
	defer span.End()

	executeDutyData, err := msg.GetExecuteCommitteeDutyData()
	if err != nil {
		err = fmt.Errorf("failed to get execute committee duty data: %w", err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	span.SetAttributes(
		observability.BeaconSlotAttribute(executeDutyData.Duty.Slot),
		observability.RunnerRoleAttribute(executeDutyData.Duty.RunnerRole()),
		observability.DutyCountAttribute(len(executeDutyData.Duty.ValidatorDuties)),
	)
	span.AddEvent("start duty")
	if err := c.StartDuty(ctx, logger, executeDutyData.Duty); err != nil {
		err = fmt.Errorf("could not start committee duty: %w", err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	span.AddEvent("start consume queue")
	if err := c.StartConsumeQueue(ctx, logger, executeDutyData.Duty); err != nil {
		err = fmt.Errorf("could not start committee consume queue: %w", err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	span.SetStatus(codes.Ok, "")
	return nil
}
