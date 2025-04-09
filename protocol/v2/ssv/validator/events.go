package validator

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

func (v *Validator) handleEventMessage(ctx context.Context, logger *zap.Logger, msg *queue.SSVMessage, dutyRunner runner.Runner) error {
	eventMsg, ok := msg.Body.(*types.EventMsg)
	if !ok {
		return fmt.Errorf("could not decode event message")
	}
	switch eventMsg.Type {
	case types.Timeout:
		if err := dutyRunner.GetBaseRunner().QBFTController.OnTimeout(ctx, logger, *eventMsg); err != nil {
			return fmt.Errorf("timeout event: %w", err)
		}
		return nil
	case types.ExecuteDuty:
		if err := v.OnExecuteDuty(ctx, logger, eventMsg); err != nil {
			return fmt.Errorf("execute duty event: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unknown event msg - %s", eventMsg.Type.String())
	}
}

func (c *Committee) handleEventMessage(ctx context.Context, logger *zap.Logger, msg *queue.SSVMessage) error {
	eventMsg, ok := msg.Body.(*types.EventMsg)
	if !ok {
		return fmt.Errorf("could not decode event message")
	}
	switch eventMsg.Type {
	case types.Timeout:
		slot, err := msg.Slot()
		if err != nil {
			return err
		}
		c.mtx.RLock()
		dutyRunner, found := c.Runners[slot]
		c.mtx.RUnlock()

		if !found {
			logger.Error("timeout event: no committee runner found for slot", fields.Slot(slot), fields.MessageID(msg.MsgID))
			return nil
		}

		if err := dutyRunner.GetBaseRunner().QBFTController.OnTimeout(ctx, logger, *eventMsg); err != nil {
			return fmt.Errorf("timeout event: %w", err)
		}
		return nil
	case types.ExecuteDuty:
		if err := c.OnExecuteDuty(ctx, logger, eventMsg); err != nil {
			return fmt.Errorf("execute duty event: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unknown event msg - %s", eventMsg.Type.String())
	}
}
