package validator

import (
	"encoding/json"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/protocol/v2/message"
	"github.com/bloxapp/ssv/protocol/v2/qbft/roundtimer"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
	"github.com/bloxapp/ssv/protocol/v2/types"
)

func (v *Validator) onTimeout(logger *zap.Logger, identifier spectypes.MessageID, height specqbft.Height) roundtimer.OnRoundTimeoutF {
	return func(round specqbft.Round) {
		v.mtx.RLock() // read-lock for v.Queues, v.state
		defer v.mtx.RUnlock()

		// only run if the validator is started
		if v.state != uint32(Started) {
			return
		}

		dr := v.DutyRunners[identifier.GetRoleType()]
		hasDuty := dr.HasRunningDuty()
		if !hasDuty {
			return
		}

		msg, err := v.createTimerMessage(identifier, height, round)
		if err != nil {
			logger.Debug("❗ failed to create timer msg", zap.Error(err))
			return
		}
		dec, err := queue.DecodeSSVMessage(msg)
		if err != nil {
			logger.Debug("❌ failed to decode timer msg", zap.Error(err))
			return
		}

		if pushed := v.Queues[identifier.GetRoleType()].Q.TryPush(dec); !pushed {
			logger.Warn("❗️ dropping timeout message because the queue is full",
				fields.Role(identifier.GetRoleType()))
		}
		// logger.Debug("📬 queue: pushed message", fields.PubKey(identifier.GetPubKey()), fields.MessageID(dec.MsgID), fields.MessageType(dec.MsgType))
	}
}

func (v *Validator) createTimerMessage(identifier spectypes.MessageID, height specqbft.Height, round specqbft.Round) (*spectypes.SSVMessage, error) {
	td := types.TimeoutData{
		Height: height,
		Round:  round,
	}
	data, err := json.Marshal(td)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal timeout data")
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
