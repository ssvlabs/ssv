package validator

import (
	"context"
	"fmt"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/protocol/v2/message"
	"github.com/bloxapp/ssv/protocol/v2/qbft/instance"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
	"github.com/bloxapp/ssv/protocol/v2/types"
)

// MessageHandler process the msg. return error if exist
type MessageHandler func(logger *zap.Logger, msg *queue.DecodedSSVMessage) error

// queueContainer wraps a queue with its corresponding state
type queueContainer struct {
	Q          queue.Queue
	queueState *queue.State
}

// HandleMessage handles a spectypes.SSVMessage.
// TODO: accept DecodedSSVMessage once p2p is upgraded to decode messages during validation.
// TODO: get rid of logger, add context
func (v *Validator) HandleMessage(logger *zap.Logger, msg *queue.DecodedSSVMessage) {
	v.mtx.RLock() // read v.Queues
	defer v.mtx.RUnlock()

	// logger.Debug("📬 handling SSV message",
	// 	zap.Uint64("type", uint64(msg.MsgType)),
	// 	fields.Role(msg.MsgID.GetRoleType()))

	if q, ok := v.Queues[msg.MsgID.GetRoleType()]; ok {
		if pushed := q.Q.TryPush(msg); !pushed {
			msgID := msg.MsgID.String()
			logger.Warn("❗ dropping message because the queue is full",
				zap.String("msg_type", message.MsgTypeToString(msg.MsgType)),
				zap.String("msg_id", msgID))
		}
		// logger.Debug("📬 queue: pushed message", fields.MessageID(msg.MsgID), fields.MessageType(msg.MsgType))
	} else {
		logger.Error("❌ missing queue for role type", fields.Role(msg.MsgID.GetRoleType()))
	}
}

// StartQueueConsumer start ConsumeQueue with handler
func (v *Validator) StartQueueConsumer(logger *zap.Logger, msgID spectypes.MessageID, handler MessageHandler) {
	ctx, cancel := context.WithCancel(v.ctx)
	defer cancel()

	for ctx.Err() == nil {
		err := v.ConsumeQueue(logger, msgID, handler)
		if err != nil {
			logger.Debug("❗ failed consuming queue", zap.Error(err))
		}
	}
}

// ConsumeQueue consumes messages from the queue.Queue of the controller
// it checks for current state
func (v *Validator) ConsumeQueue(logger *zap.Logger, msgID spectypes.MessageID, handler MessageHandler) error {
	ctx, cancel := context.WithCancel(v.ctx)
	defer cancel()

	var q queueContainer
	err := func() error {
		v.mtx.RLock() // read v.Queues
		defer v.mtx.RUnlock()
		var ok bool
		q, ok = v.Queues[msgID.GetRoleType()]
		if !ok {
			return errors.New(fmt.Sprintf("queue not found for role %s", msgID.GetRoleType().String()))
		}
		return nil
	}()
	if err != nil {
		return err
	}

	logger.Debug("📬 queue consumer is running")

	lens := make([]int, 0, 10)

	for ctx.Err() == nil {
		// Construct a representation of the current state.
		state := *q.queueState
		runner := v.DutyRunners.DutyRunnerForMsgID(msgID)
		if runner == nil {
			return fmt.Errorf("could not get duty runner for msg ID %v", msgID)
		}
		var runningInstance *instance.Instance
		if runner.HasRunningDuty() {
			runningInstance = runner.GetBaseRunner().State.RunningInstance
			if runningInstance != nil {
				decided, _ := runningInstance.IsDecided()
				state.HasRunningInstance = !decided
			}
		}
		state.Height = v.GetLastHeight(msgID)
		state.Round = v.GetLastRound(msgID)
		state.Quorum = v.Share.Quorum

		filter := queue.FilterAny
		if !runner.HasRunningDuty() {
			// If no duty is running, pop only ExecuteDuty messages.
			filter = func(m *queue.DecodedSSVMessage) bool {
				e, ok := m.Body.(*types.EventMsg)
				if !ok {
					return false
				}
				return e.Type == types.ExecuteDuty
			}
		} else if runningInstance != nil && runningInstance.State.ProposalAcceptedForCurrentRound == nil {
			// If no proposal was accepted for the current round, skip prepare & commit messages
			// for the current height and round.
			filter = func(m *queue.DecodedSSVMessage) bool {
				sm, ok := m.Body.(*specqbft.SignedMessage)
				if !ok {
					return true
				}
				if sm.Message.Height != state.Height || sm.Message.Round != state.Round {
					return true
				}
				return sm.Message.MsgType != specqbft.PrepareMsgType && sm.Message.MsgType != specqbft.CommitMsgType
			}
		}

		// Pop the highest priority message for the current state.
		msg := q.Q.Pop(ctx, queue.NewMessagePrioritizer(&state), filter)
		if ctx.Err() != nil {
			break
		}
		if msg == nil {
			logger.Error("❗ got nil message from queue, but context is not done!")
			break
		}
		lens = append(lens, q.Q.Len())
		if len(lens) >= 10 {
			logger.Debug("📬 [TEMPORARY] queue statistics",
				fields.MessageID(msg.MsgID), fields.MessageType(msg.MsgType),
				zap.Ints("past_10_lengths", lens))
			lens = lens[:0]
		}

		// Handle the message.
		if err := handler(logger, msg); err != nil {
			v.logMsg(logger, msg, "❗ could not handle message",
				fields.MessageType(msg.SSVMessage.MsgType),
				zap.Error(err))
		}
	}

	logger.Debug("📪 queue consumer is closed")
	return nil
}

func (v *Validator) logMsg(logger *zap.Logger, msg *queue.DecodedSSVMessage, logMsg string, withFields ...zap.Field) {
	baseFields := []zap.Field{}
	switch msg.SSVMessage.MsgType {
	case spectypes.SSVConsensusMsgType:
		sm := msg.Body.(*specqbft.SignedMessage)
		baseFields = []zap.Field{
			zap.Int64("msg_height", int64(sm.Message.Height)),
			zap.Int64("msg_round", int64(sm.Message.Round)),
			zap.Int64("consensus_msg_type", int64(sm.Message.MsgType)),
			zap.Any("signers", sm.Signers),
		}
	case spectypes.SSVPartialSignatureMsgType:
		psm := msg.Body.(*spectypes.SignedPartialSignatureMessage)
		baseFields = []zap.Field{
			zap.Int64("signer", int64(psm.Signer)),
			fields.Slot(psm.Message.Slot),
		}
	}
	logger.Debug(logMsg, append(baseFields, withFields...)...)
}

// GetLastHeight returns the last height for the given identifier
func (v *Validator) GetLastHeight(identifier spectypes.MessageID) specqbft.Height {
	r := v.DutyRunners.DutyRunnerForMsgID(identifier)
	if r == nil {
		return specqbft.Height(0)
	}
	if ctrl := r.GetBaseRunner().QBFTController; ctrl != nil {
		return ctrl.Height
	}
	return specqbft.Height(0)
}

// GetLastRound returns the last height for the given identifier
func (v *Validator) GetLastRound(identifier spectypes.MessageID) specqbft.Round {
	r := v.DutyRunners.DutyRunnerForMsgID(identifier)
	if r == nil {
		return specqbft.Round(1)
	}
	if r != nil && r.HasRunningDuty() {
		inst := r.GetBaseRunner().State.RunningInstance
		if inst != nil {
			return inst.State.Round
		}
	}
	return specqbft.Round(1)
}
