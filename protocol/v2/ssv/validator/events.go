package validator

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/codes"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

func (v *Validator) handleEventMessage(ctx context.Context, logger *zap.Logger, msg *queue.SSVMessage, dutyRunner runner.Runner) error {
	ctx, span := tracer.Start(ctx, fmt.Sprintf("%s.handle_event_message", observabilityNamespace))
	defer span.End()

	eventMsg, ok := msg.Body.(*types.EventMsg)
	if !ok {
		err := fmt.Errorf("could not decode event message")
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	span.SetAttributes(observability.ValidatorEventTypeAttribute(eventMsg.Type))

	switch eventMsg.Type {
	case types.Timeout:
		if err := dutyRunner.GetBaseRunner().QBFTController.OnTimeout(ctx, logger, *eventMsg); err != nil {
			err := fmt.Errorf("timeout event: %w", err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		span.SetStatus(codes.Ok, "")
		return nil
	case types.ExecuteDuty:
		if err := v.OnExecuteDuty(ctx, logger, eventMsg); err != nil {
			err := fmt.Errorf("execute duty event: %w", err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		span.SetStatus(codes.Ok, "")
		return nil
	default:
		err := fmt.Errorf("unknown event msg - %s", eventMsg.Type.String())
		span.SetStatus(codes.Error, err.Error())
		return err
	}
}

func (c *Committee) handleEventMessage(ctx context.Context, logger *zap.Logger, msg *queue.SSVMessage) error {
	ctx, span := tracer.Start(ctx, fmt.Sprintf("%s.handle_committee_event_message", observabilityNamespace))
	defer span.End()

	eventMsg, ok := msg.Body.(*types.EventMsg)
	if !ok {
		err := fmt.Errorf("could not decode event message")
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	span.SetAttributes(observability.ValidatorEventTypeAttribute(eventMsg.Type))

	switch eventMsg.Type {
	case types.Timeout:
		slot, err := msg.Slot()
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		c.mtx.RLock()
		dutyRunner, found := c.Runners[slot]
		c.mtx.RUnlock()

		if !found {
			const errMsg = "no committee runner or queue found for slot"
			logger.Error(errMsg, fields.Slot(slot), fields.MessageID(msg.MsgID))
			span.SetStatus(codes.Error, errMsg)
			return nil
		}

		if err := dutyRunner.GetBaseRunner().QBFTController.OnTimeout(ctx, logger, *eventMsg); err != nil {
			err := fmt.Errorf("timeout event: %w", err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		span.SetStatus(codes.Ok, "")
		return nil
	case types.ExecuteDuty:
		if err := c.OnExecuteDuty(ctx, logger, eventMsg); err != nil {
			err := fmt.Errorf("execute duty event: %w", err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		span.SetStatus(codes.Ok, "")
		return nil
	default:
		err := fmt.Errorf("unknown event msg - %s", eventMsg.Type.String())
		span.SetStatus(codes.Error, err.Error())
		return err
	}
}
