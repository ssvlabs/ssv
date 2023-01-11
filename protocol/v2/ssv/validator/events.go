package validator

import (
	"fmt"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/protocol/v2/ssv/runner"
	"github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

func (v *Validator) handleEventMessage(msg *spectypes.SSVMessage, dutyRunner runner.Runner) error {
	eventMsg := types.EventMsg{}
	if err := eventMsg.Decode(msg.GetData()); err != nil {
		return errors.Wrap(err, "could not get event Message from network Message")
	}
	switch eventMsg.Type {
	case types.Timeout:
		err := dutyRunner.GetBaseRunner().QBFTController.OnTimeout(eventMsg)
		if err != nil {
			logger.Warn("on timeout failed", zap.Error(err)) // need to return error instead?
		}
		return nil
	case types.ExecuteDuty:
		err := v.OnExecuteDuty(eventMsg)
		if err != nil {
			logger.Warn("failed to execute duty", zap.Error(err)) // need to return error instead?
		}
		return nil
	default:
		return errors.New(fmt.Sprintf("unknown event msg - %s", eventMsg.Type.String()))
	}
}
