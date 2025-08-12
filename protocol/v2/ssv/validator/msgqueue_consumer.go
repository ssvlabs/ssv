package validator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jellydator/ttlcache/v3"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/protocol/v2/message"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

// MessageHandler process the msg. return error if exist
type MessageHandler func(ctx context.Context, logger *zap.Logger, msg *queue.SSVMessage) error

// QueueContainer wraps a queue with its corresponding state
type QueueContainer struct {
	Q          queue.Queue
	queueState *queue.State
}

// HandleMessage handles a spectypes.SSVMessage.
// TODO: accept DecodedSSVMessage once p2p is upgraded to decode messages during validation.
// TODO: get rid of logger, add context
func (v *Validator) HandleMessage(ctx context.Context, logger *zap.Logger, msg *queue.SSVMessage) {
	traceCtx, dutyID := v.fetchTraceContext(ctx, msg.GetID())
	ctx, span := tracer.Start(traceCtx,
		observability.InstrumentName(observabilityNamespace, "handle_message"),
		trace.WithAttributes(
			observability.ValidatorMsgIDAttribute(msg.GetID()),
			observability.ValidatorMsgTypeAttribute(msg.GetType()),
			observability.RunnerRoleAttribute(msg.GetID().GetRoleType()),
			observability.DutyIDAttribute(dutyID),
		))
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
	} else {
		const errMsg = "❌ missing queue for role type"
		logger.Error(errMsg, fields.Role(msg.MsgID.GetRoleType()))
		span.SetStatus(codes.Error, errMsg)
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

	logger.Debug("📬 queue consumer is running")
	defer logger.Debug("📪 queue consumer is closed")

	lens := make([]int, 0, 10)

	type retryIDType string
	retryID := func(msg *queue.SSVMessage) retryIDType {
		sig := "undefined"
		if msg.MsgType == spectypes.SSVConsensusMsgType {
			signers := msg.SignedSSVMessage.OperatorIDs
			sig = strings.Trim(strings.Join(strings.Fields(fmt.Sprint(signers)), "-"), "[]")
		}
		if msg.MsgType == spectypes.SSVPartialSignatureMsgType {
			psm := msg.Body.(*spectypes.PartialSignatureMessages)
			signer := psm.Messages[0].Signer // same signer for all messages
			sig = fmt.Sprintf("%d", signer)
		}
		return retryIDType(fmt.Sprintf("%s-%s", msg.MsgID, sig))
	}
	// msgRetries keeps track of how many times we've tried to handle a particular message. Since this map
	// grows over time, we need to clean it up automatically. There is no specific TTL value to use for its
	// entries - it just needs to be large enough to prevent unnecessary (but non-harmful) retries from happening.
	msgRetries := ttlcache.New(
		ttlcache.WithTTL[retryIDType, int](10 * time.Minute),
	)
	go msgRetries.Start()

	for ctx.Err() == nil {
		// Construct a representation of the current state.
		state := *q.queueState
		r := v.DutyRunners.DutyRunnerForMsgID(msgID)
		if r == nil {
			return fmt.Errorf("could not get duty runner for msg ID %v", msgID)
		}
		var runningInstance *instance.Instance
		if r.HasRunningDuty() {
			runningInstance = r.GetBaseRunner().State.RunningInstance
			if runningInstance != nil {
				decided, _ := runningInstance.IsDecided()
				state.HasRunningInstance = !decided
			}
		}
		state.Height = v.GetLastHeight(msgID)
		state.Round = v.GetLastRound(msgID)
		state.Quorum = v.Operator.GetQuorum()

		filter := queue.FilterAny
		if !r.HasRunningDuty() {
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
			return nil
		}
		if msg == nil {
			logger.Error("❗ got nil message from queue, but context is not done!")
			return nil
		}
		lens = append(lens, q.Q.Len())
		if len(lens) >= 10 {
			logger.Debug("📬 [TEMPORARY] queue statistics",
				fields.MessageID(msg.MsgID), fields.MessageType(msg.MsgType),
				zap.Ints("past_10_lengths", lens))
			lens = lens[:0]
		}

		// Handle the message, potentially scheduling a message-replay for later.
		err = handler(ctx, logger, msg)
		if err != nil {
			const (
				retryDelay = 10 * time.Millisecond
				retryCount = 99
			)
			msgRetryItem := msgRetries.Get(retryID(msg))
			if msgRetryItem == nil {
				msgRetries.Set(retryID(msg), 0, ttlcache.DefaultTTL)
				msgRetryItem = msgRetries.Get(retryID(msg))
			}
			msgRetryCnt := msgRetryItem.Value()

			// TODO
			logger.Debug("validator: printing msgRetries size", zap.Int("size", msgRetries.Len()))

			logMsg := "❗ could not handle message"

			switch {
			case (errors.Is(err, runner.ErrNoRunningDuty) || errors.Is(err, runner.ErrInvalidPartialSigSlot) ||
				errors.Is(err, runner.ErrInstanceNotFound) || errors.Is(err, runner.ErrFutureMsg) ||
				errors.Is(err, runner.ErrWrongMsgHeight) || errors.Is(err, runner.ErrNoProposalForRound) ||
				errors.Is(err, runner.ErrWrongMsgRound) || errors.Is(err, runner.ErrNoDecidedValue)) && msgRetryCnt < retryCount:

				logMsg += fmt.Sprintf(", retrying message in ~%dms", retryDelay.Milliseconds())
				msgRetries.Set(retryID(msg), msgRetryCnt+1, ttlcache.DefaultTTL)
				go func() {
					time.Sleep(retryDelay)
					q.Q.Push(msg)
				}()
			default:
				logMsg += ", dropping message"
			}

			v.logMsg(
				logger,
				msg,
				logMsg,
				fields.MessageID(msg.MsgID),
				fields.MessageType(msg.MsgType),
				zap.Error(err),
				zap.Int("attempt", msgRetryCnt+1),
			)
		}
	}

	return nil
}

func (v *Validator) logMsg(logger *zap.Logger, msg *queue.SSVMessage, logMsg string, withFields ...zap.Field) {
	var baseFields []zap.Field
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
		baseFields = []zap.Field{
			zap.Uint64("signer", psm.Messages[0].Signer), // same signer for all messages
			fields.Slot(psm.Slot),
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
	if r.HasRunningDuty() {
		inst := r.GetBaseRunner().State.RunningInstance
		if inst != nil {
			return inst.State.Round
		}
	}
	return specqbft.Round(1)
}
