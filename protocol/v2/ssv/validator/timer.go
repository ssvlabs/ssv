package validator

import (
	"context"
	"encoding/json"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/protocol/v2/message"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

// newQBFTRoundTimerF returns a QBFTRoundTimerF that builds a RoundTimer for a QBFT instance and wires
// round-timeouts back into the validator's per-role message queue as event messages. The returned factory
// closes over identifier so each DutyRunner gets a factory bound to its own msg ID.
func (v *Validator) newQBFTRoundTimerF(runnerIdentifier spectypes.MessageID) ssv.QBFTRoundTimerF {
	return func(ctx context.Context, logger *zap.Logger, height specqbft.Height) ssv.QBFTRoundTimer {
		callback := func(round specqbft.Round) {
			v.mtx.RLock() // read-lock for v.Queues
			defer v.mtx.RUnlock()

			// If the relevant queue hasn't been initialized yet, there isn't a running duty we can issue a
			// timeout for, in practice this should never happen - but we need to handle this just in case.
			q := v.Queues[runnerIdentifier.GetRoleType()]
			if q == nil {
				logger.Error("❗ couldn't schedule timeout event due to missing queue")
				return
			}

			msg, err := v.createTimerMessage(runnerIdentifier, height, round)
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
				logger.Error("❗️ dropping timeout message because the queue is full", fields.RunnerRole(runnerIdentifier.GetRoleType()))
				return
			}
		}
		return roundtimer.New(ctx, v.NetworkConfig.Beacon, runnerIdentifier.GetRoleType(), height, callback)
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

// newQBFTRoundTimerF returns a QBFTRoundTimerF that builds a RoundTimer for a Committee QBFT instance and
// wires round-timeouts back into the slot-keyed queue as event messages. The returned factory closes over
// identifier so each runner gets a factory bound to the committee's msg ID.
func (c *Committee) newQBFTRoundTimerF(runnerIdentifier spectypes.MessageID) ssv.QBFTRoundTimerF {
	return func(ctx context.Context, logger *zap.Logger, height specqbft.Height) ssv.QBFTRoundTimer {
		callback := func(round specqbft.Round) {
			c.mtx.RLock() // read-lock for c.Queues
			defer c.mtx.RUnlock()

			// If the relevant queue hasn't been initialized yet, there isn't a running duty we can issue a
			// timeout for, in practice this should never happen - but we need to handle this just in case.
			// This is also possible if the queue got pruned already (due to becoming old and irrelevant).
			q := c.Queues[phase0.Slot(height)]
			if q.Q == nil {
				logger.Debug("couldn't schedule timeout event due to missing queue (likely was pruned)")
				return
			}

			msg, err := c.createTimerMessage(runnerIdentifier, height, round)
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
				logger.Error("❗️ dropping timeout message because the queue is full", fields.RunnerRole(runnerIdentifier.GetRoleType()))
			}
		}
		return roundtimer.New(ctx, c.networkConfig.Beacon, runnerIdentifier.GetRoleType(), height, callback)
	}
}

func (c *Committee) createTimerMessage(identifier spectypes.MessageID, height specqbft.Height, round specqbft.Round) (*spectypes.SSVMessage, error) {
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
