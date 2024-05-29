package validator

import (
	"fmt"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

func (v *Validator) handleEventMessage(logger *zap.Logger, msg *queue.DecodedSSVMessage, dutyRunner runner.Runner) error {
	eventMsg, ok := msg.Body.(*types.EventMsg)
	if !ok {
		return errors.New("could not decode event message")
	}
	switch eventMsg.Type {
	case types.Timeout:
		if err := dutyRunner.GetBaseRunner().QBFTController.OnTimeout(logger, *eventMsg); err != nil {
			return fmt.Errorf("timeout event: %w", err)
		}
		return nil
	case types.ExecuteDuty:
		if err := v.OnExecuteDuty(logger, *eventMsg); err != nil {
			return fmt.Errorf("execute duty event: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unknown event msg - %s", eventMsg.Type.String())
	}
}
