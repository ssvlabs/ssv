package validator

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/protocol/v2/message"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

func (v *Validator) onTimeout(ctx context.Context, logger *zap.Logger, identifier spectypes.MessageID, slot phase0.Slot) roundtimer.OnRoundTimeoutF {
	return func(round specqbft.Round) {
		v.mtx.RLock() // read-lock for v.Queues
		defer v.mtx.RUnlock()

		// If the relevant queue hasn't been initialized yet, there isn't a running duty we can issue a
		// timeout for, in practice this should never happen - but we need to handle this just in case.
		q := v.Queues[identifier.GetRoleType()]
		if q == nil {
			logger.Error("❗ couldn't schedule timeout event due to missing queue")
			return
		}

		msg, err := v.createTimerMessage(identifier, slot, round)
		if err != nil {
			logger.Error("❌ failed to create timer msg", zap.Error(err))
			return
		}
		dec, err := queue.DecodeSSVMessage(msg)
		if err != nil {
			logger.Error("❌ failed to decode timer msg", zap.Error(err))
			return
		}

		if pushed := q.TryPush(dec); !pushed {
			logger.Error("❗️ dropping timeout message because the queue is full", fields.RunnerRole(identifier.GetRoleType()))
			return
		}
	}
}

func (v *Validator) createTimerMessage(identifier spectypes.MessageID, slot phase0.Slot, round specqbft.Round) (*spectypes.SSVMessage, error) {
	td := types.TimeoutData{
		Slot:  slot,
		Round: round,
	}
	data, err := json.Marshal(td)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal timeout data: %w", err)
	}
	eventMsg := &types.EventMsg{
		Type: types.Timeout,
		Data: data,
	}

	eventMsgData, err := eventMsg.Encode()
	if err != nil {
		return nil, fmt.Errorf("failed to encode timeout signed msg: %w", err)
	}
	return &spectypes.SSVMessage{
		MsgType: message.SSVEventMsgType,
		MsgID:   identifier,
		Data:    eventMsgData,
	}, nil
}

func (c *Committee) onTimeout(ctx context.Context, logger *zap.Logger, identifier spectypes.MessageID, slot phase0.Slot) roundtimer.OnRoundTimeoutF {
	return func(round specqbft.Round) {
		c.mtx.RLock() // read-lock for c.Queues
		defer c.mtx.RUnlock()

		// If the relevant queue hasn't been initialized yet, there isn't a running duty we can issue a
		// timeout for, in practice this should never happen - but we need to handle this just in case.
		// This is also possible if the queue got pruned already (due to becoming old and irrelevant).
		q := c.Queues[slot]
		if q.Q == nil {
			logger.Debug("couldn't schedule timeout event due to missing queue (likely was pruned)")
			return
		}

		msg, err := c.createTimerMessage(identifier, slot, round)
		if err != nil {
			logger.Error("❌ failed to create timer msg", zap.Error(err))
			return
		}
		dec, err := queue.DecodeSSVMessage(msg)
		if err != nil {
			logger.Error("❌ failed to decode timer msg", zap.Error(err))
			return
		}

		if pushed := q.Q.TryPush(dec); !pushed {
			logger.Error("❗️ dropping timeout message because the queue is full", fields.RunnerRole(identifier.GetRoleType()))
		}
	}
}

func (c *Committee) createTimerMessage(identifier spectypes.MessageID, slot phase0.Slot, round specqbft.Round) (*spectypes.SSVMessage, error) {
	td := types.TimeoutData{
		Slot:  slot,
		Round: round,
	}
	data, err := json.Marshal(td)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal timeout data: %w", err)
	}
	eventMsg := &types.EventMsg{
		Type: types.Timeout,
		Data: data,
	}

	eventMsgData, err := eventMsg.Encode()
	if err != nil {
		return nil, fmt.Errorf("failed to encode timeout signed msg: %w", err)
	}
	return &spectypes.SSVMessage{
		MsgType: message.SSVEventMsgType,
		MsgID:   identifier,
		Data:    eventMsgData,
	}, nil
}
