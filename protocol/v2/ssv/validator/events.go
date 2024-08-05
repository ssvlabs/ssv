package validator

import (
	"fmt"

	"github.com/ssvlabs/ssv/logging/fields"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

func (v *Validator) handleEventMessage(logger *zap.Logger, msg *queue.SSVMessage, dutyRunner runner.Runner) error {
	eventMsg, ok := msg.Body.(*types.EventMsg)
	if !ok {
		return fmt.Errorf("could not decode event message")
	}
	switch eventMsg.Type {
	case types.Timeout:
		if err := dutyRunner.GetBaseRunner().QBFTController.OnTimeout(logger, *eventMsg); err != nil {
			return fmt.Errorf("timeout event: %w", err)
		}
		return nil
	case types.ExecuteDuty:
		if err := v.OnExecuteDuty(logger, eventMsg); err != nil {
			return fmt.Errorf("execute duty event: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unknown event msg - %s", eventMsg.Type.String())
	}
}

func (c *Committee) handleEventMessage(logger *zap.Logger, msg *queue.SSVMessage) error {
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
		dutyRunner, found := c.Runners[slot]
		c.mtx.Unlock()

		if !found {
			logger.Error("no committee runner or queue found for slot", fields.Slot(slot), fields.MessageID(msg.MsgID))
			return nil
		}

		if err := dutyRunner.GetBaseRunner().QBFTController.OnTimeout(logger, *eventMsg); err != nil {
			return fmt.Errorf("timeout event: %w", err)
		}
		return nil
	case types.ExecuteDuty:
		if err := c.OnExecuteDuty(logger, eventMsg); err != nil {
			return fmt.Errorf("execute duty event: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unknown event msg - %s", eventMsg.Type.String())
	}
}
