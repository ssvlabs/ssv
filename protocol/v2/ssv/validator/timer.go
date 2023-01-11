package validator

import (
	"encoding/json"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/protocol/v2/message"
	"github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

func (v *Validator) onTimeout(identifier spectypes.MessageID, height specqbft.Height) func() {
	return func() {
		dr := v.DutyRunners[identifier.GetRoleType()]
		if !dr.HasRunningDuty() {
			return
		}

		msg, err := v.createTimerMessage(identifier, height)
		if err != nil {
			v.logger.Warn("failed to create timer msg", zap.Error(err))
			return
		}
		v.Queues[identifier.GetRoleType()].Add(msg)
	}
}

func (v *Validator) createTimerMessage(identifier spectypes.MessageID, height specqbft.Height) (*spectypes.SSVMessage, error) {
	td := types.TimeoutData{Height: height}
	data, err := json.Marshal(td)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal timout data")
	}
	eventMsg := &types.EventMsg{
		Type: types.Timeout,
		Data: data,
	}

	eventMsgData, err := eventMsg.Encode()
	if err != nil {
		return nil, errors.Wrap(err, "failed to encode timeout signed msg")
	}
	return &spectypes.SSVMessage{
		MsgType: message.SSVEventMsgType,
		MsgID:   identifier,
		Data:    eventMsgData,
	}, nil
}
