package validator

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/observability/traces"
	"github.com/ssvlabs/ssv/protocol/v2/message"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

// MessageHandler process the msg. return error if exist
type MessageHandler func(ctx context.Context, msg *queue.SSVMessage) error

// QueueContainer wraps a queue with its corresponding state
type QueueContainer struct {
	Q          queue.Queue
	queueState *queue.State
}

// EnqueueMessage enqueues a spectypes.SSVMessage for processing.
// TODO: accept DecodedSSVMessage once p2p is upgraded to decode messages during validation.
func (v *Validator) EnqueueMessage(ctx context.Context, msg *queue.SSVMessage) {
	msgType := msg.GetType()
	msgID := msg.GetID()

	logger := v.logger.
		With(fields.MessageType(msgType)).
		With(fields.MessageID(msgID)).
		With(fields.RunnerRole(msgID.GetRoleType()))

	slot, err := msg.Slot()
	if err != nil {
		logger.Error("❌ couldn't get message slot", zap.Error(err))
		return
	}
	dutyID := fields.BuildDutyID(v.NetworkConfig.EstimatedEpochAtSlot(slot), slot, msgID.GetRoleType(), v.Share.ValidatorIndex)

	logger = logger.
		With(fields.Slot(slot)).
		With(fields.DutyID(dutyID))

	ctx, span := tracer.Start(traces.Context(ctx, dutyID),
		observability.InstrumentName(observabilityNamespace, "enqueue_validator_message"),
		trace.WithAttributes(
			observability.ValidatorMsgTypeAttribute(msgType),
			observability.ValidatorMsgIDAttribute(msgID),
			observability.RunnerRoleAttribute(msgID.GetRoleType()),
			observability.BeaconSlotAttribute(slot),
			observability.DutyIDAttribute(dutyID)))
	defer span.End()

	msg.TraceContext = ctx

	v.mtx.RLock() // read v.Queues
	defer v.mtx.RUnlock()
	if q, ok := v.Queues[msg.MsgID.GetRoleType()]; ok {
		span.AddEvent("pushing message to queue")
		if pushed := q.Q.TryPush(msg); !pushed {
			const eventMsg = "❗ dropping message because the queue is full"
			logger.Warn(eventMsg,
				zap.String("msg_type", message.MsgTypeToString(msg.MsgType)),
				zap.String("msg_id", msg.MsgID.String()))

			span.AddEvent(eventMsg)
		}
		span.SetStatus(codes.Ok, "")
		return
	}

	const errMsg = "❌ missing queue for role type"
	logger.Error(errMsg, fields.RunnerRole(msg.MsgID.GetRoleType()))
	span.SetStatus(codes.Error, errMsg)
}

// StartQueueConsumer start ConsumeQueue with handler
func (v *Validator) StartQueueConsumer(msgID spectypes.MessageID, handler MessageHandler) {
	ctx, cancel := context.WithCancel(v.ctx)
	defer cancel()

	for ctx.Err() == nil {
		err := v.ConsumeQueue(msgID, handler)
		if err != nil {
			v.logger.Debug("❗ failed consuming queue", zap.Error(err))
		}
	}
}

// ConsumeQueue consumes messages from the queue.Queue of the controller
// it checks for current state
func (v *Validator) ConsumeQueue(msgID spectypes.MessageID, handler MessageHandler) error {
	ctx, cancel := context.WithCancel(v.ctx)
	defer cancel()

	var q QueueContainer
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

	v.logger.Debug("📬 queue consumer is running")

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
		state.Quorum = v.Operator.GetQuorum()

		filter := queue.FilterAny
		if !runner.HasRunningDuty() {
			// If no duty is running, pop only ExecuteDuty messages.
			filter = func(m *queue.SSVMessage) bool {
				e, ok := m.Body.(*types.EventMsg)
				if !ok {
					return false
				}
				return e.Type == types.ExecuteDuty
			}
		} else if runningInstance != nil && runningInstance.State.ProposalAcceptedForCurrentRound == nil {
			// If no proposal was accepted for the current round, skip prepare & commit messages
			// for the current height and round.
			filter = func(m *queue.SSVMessage) bool {
				qbftMsg, ok := m.Body.(*specqbft.Message)
				if !ok {
					return true
				}

				if qbftMsg.Height != state.Height || qbftMsg.Round != state.Round {
					return true
				}
				return qbftMsg.MsgType != specqbft.PrepareMsgType && qbftMsg.MsgType != specqbft.CommitMsgType
			}
		}

		// Pop the highest priority message for the current state.
		msg := q.Q.Pop(ctx, queue.NewMessagePrioritizer(&state), filter)
		if ctx.Err() != nil {
			break
		}
		if msg == nil {
			v.logger.Error("❗ got nil message from queue, but context is not done!")
			break
		}
		lens = append(lens, q.Q.Len())
		if len(lens) >= 10 {
			v.logger.Debug("📬 [TEMPORARY] queue statistics",
				fields.MessageID(msg.MsgID), fields.MessageType(msg.MsgType),
				zap.Ints("past_10_lengths", lens))
			lens = lens[:0]
		}

		// Handle the message.
		if err := handler(ctx, msg); err != nil {
			v.logMsg(msg, "❗ could not handle message",
				fields.MessageType(msg.MsgType),
				zap.Error(err))
		}
	}

	v.logger.Debug("📪 queue consumer is closed")
	return nil
}

func (v *Validator) logMsg(msg *queue.SSVMessage, logMsg string, withFields ...zap.Field) {
	baseFields := []zap.Field{}
	if msg.MsgType == spectypes.SSVConsensusMsgType {
		qbftMsg := msg.Body.(*specqbft.Message)
		baseFields = []zap.Field{
			zap.Uint64("msg_height", uint64(qbftMsg.Height)),
			zap.Uint64("msg_round", uint64(qbftMsg.Round)),
			zap.Uint64("consensus_msg_type", uint64(qbftMsg.MsgType)),
			zap.Any("signers", msg.SignedSSVMessage.OperatorIDs),
		}
	}
	if msg.MsgType == spectypes.SSVPartialSignatureMsgType {
		psm := msg.Body.(*spectypes.PartialSignatureMessages)
		// signer must be same for all messages, at least 1 message must be present (this is validated prior)
		signer := psm.Messages[0].Signer
		baseFields = []zap.Field{
			zap.Uint64("signer", signer),
			fields.Slot(psm.Slot),
		}
	}
	v.logger.Debug(logMsg, append(baseFields, withFields...)...)
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
	if r.HasRunningDuty() {
		inst := r.GetBaseRunner().State.RunningInstance
		if inst != nil {
			return inst.State.Round
		}
	}
	return specqbft.Round(1)
}
