package validator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jellydator/ttlcache/v3"
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
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

// HandleMessage handles a spectypes.SSVMessage.
// TODO: accept DecodedSSVMessage once p2p is upgraded to decode messages during validation.
// TODO: get rid of logger, add context
func (c *Committee) HandleMessage(ctx context.Context, logger *zap.Logger, msg *queue.SSVMessage) {
	slot, err := msg.Slot()
	if err != nil {
		logger.Error("❌ could not get slot from message", fields.MessageID(msg.MsgID), zap.Error(err))
		return
	}
	dutyID := fields.FormatCommitteeDutyID(types.OperatorIDsFromOperators(c.CommitteeMember.Committee), c.beaconConfig.EstimatedEpochAtSlot(slot), slot)
	ctx, span := tracer.Start(traces.Context(ctx, dutyID),
		observability.InstrumentName(observabilityNamespace, "handle_committee_message"),
		trace.WithAttributes(
			observability.ValidatorMsgIDAttribute(msg.GetID()),
			observability.ValidatorMsgTypeAttribute(msg.GetType()),
			observability.RunnerRoleAttribute(msg.GetID().GetRoleType()),
			observability.DutyIDAttribute(dutyID),
		))
	defer span.End()

	msg.TraceContext = ctx
	span.SetAttributes(observability.BeaconSlotAttribute(slot))
	// Retrieve or create the queue for the given slot.
	c.mtx.Lock()
	q, ok := c.Queues[slot]
	if !ok {
		q = QueueContainer{
			Q: queue.New(1000), // TODO alan: get queue opts from options
			queueState: &queue.State{
				HasRunningInstance: false,
				Height:             specqbft.Height(slot),
				Slot:               slot,
				//Quorum:             options.SSVShare.Share,// TODO
			},
		}
		c.Queues[slot] = q
		const eventMsg = "missing queue for slot created"
		logger.Debug(eventMsg, fields.Slot(slot))
		span.AddEvent(eventMsg)
	}
	c.mtx.Unlock()

	span.AddEvent("pushing message to the queue")
	if pushed := q.Q.TryPush(msg); !pushed {
		const errMsg = "❗ dropping message because the queue is full"
		logger.Warn(errMsg,
			zap.String("msg_type", message.MsgTypeToString(msg.MsgType)),
			zap.String("msg_id", msg.MsgID.String()))
		span.SetStatus(codes.Error, errMsg)
	} else {
		span.SetStatus(codes.Ok, "")
	}
}

func (c *Committee) StartConsumeQueue(ctx context.Context, logger *zap.Logger, duty *spectypes.CommitteeDuty) error {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	// Setting the cancel function separately due the queue could be created in HandleMessage
	q, found := c.Queues[duty.Slot]
	if !found {
		return fmt.Errorf("no queue found for slot %d", duty.Slot)
	}

	r := c.Runners[duty.Slot]
	if r == nil {
		return fmt.Errorf("no runner found for slot %d", duty.Slot)
	}

	// required to stop the queue consumer when timeout message is received by handler
	queueCtx, cancelF := context.WithDeadline(c.ctx, c.beaconConfig.EstimatedTimeAtSlot(duty.Slot+runnerExpirySlots))

	go func() {
		defer cancelF()
		c.ConsumeQueue(queueCtx, q, logger, c.ProcessMessage, r)
	}()

	return nil
}

// ConsumeQueue consumes messages from the queue.Queue of the controller
// it checks for current state
func (c *Committee) ConsumeQueue(
	ctx context.Context,
	q QueueContainer,
	logger *zap.Logger,
	handler MessageHandler,
	rnr *runner.CommitteeRunner,
) {
	logger.Debug("📬 queue consumer is running")
	defer logger.Debug("📪 queue consumer is closed")

	state := *q.queueState
	lens := make([]int, 0, 10)

	type retryIDType string
	// messageID returns an ID that represents a potentially retryable message (msg.ID is the same for messages
	// with different signers - so we can't use that for retries)
	messageID := func(msg *queue.SSVMessage) retryIDType {
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
		var runningInstance *instance.Instance
		if rnr.HasRunningDuty() {
			runningInstance = rnr.GetBaseRunner().State.RunningInstance
			if runningInstance != nil {
				decided, _ := runningInstance.IsDecided()
				state.HasRunningInstance = !decided
			}
		}

		filter := queue.FilterAny
		if runningInstance != nil && runningInstance.State.ProposalAcceptedForCurrentRound == nil {
			// If no proposal was accepted for the current round, skip prepare & commit messages
			// for the current round.
			filter = func(m *queue.SSVMessage) bool {
				sm, ok := m.Body.(*specqbft.Message)
				if !ok {
					return m.MsgType != spectypes.SSVPartialSignatureMsgType
				}

				if sm.Round != state.Round { // allow next round or change round messages.
					return true
				}

				return sm.MsgType != specqbft.PrepareMsgType && sm.MsgType != specqbft.CommitMsgType
			}
		} else if runningInstance != nil && !runningInstance.State.Decided {
			filter = func(ssvMessage *queue.SSVMessage) bool {
				// don't read post consensus until decided
				return ssvMessage.MsgType != spectypes.SSVPartialSignatureMsgType
			}
		}

		// Pop the highest priority message for the current state.
		// TODO: (Alan) bring back filter
		msg := q.Q.Pop(ctx, queue.NewCommitteeQueuePrioritizer(&state), filter)
		if ctx.Err() != nil {
			return
		}
		if msg == nil {
			logger.Error("❗ got nil message from queue, but context is not done!")
			return
		}
		lens = append(lens, q.Q.Len())
		if len(lens) >= 10 {
			logger.Debug("📬 [TEMPORARY] queue statistics",
				fields.MessageID(msg.MsgID), fields.MessageType(msg.MsgType),
				zap.Ints("past_10_lengths", lens))
			lens = lens[:0]
		}

		// Handle the message, potentially scheduling a message-replay for later.
		err := handler(ctx, logger, msg)
		if err != nil {
			const (
				retryDelay = 10 * time.Millisecond
				retryCount = 99
			)
			msgRetryItem := msgRetries.Get(messageID(msg))
			if msgRetryItem == nil {
				msgRetries.Set(messageID(msg), 0, ttlcache.DefaultTTL)
				msgRetryItem = msgRetries.Get(messageID(msg))
			}
			msgRetryCnt := msgRetryItem.Value()

			// TODO
			logger.Debug("committee: printing msgRetries size", zap.Int("size", msgRetries.Len()))

			logMsg := "❗ could not handle message"

			switch {
			case errors.Is(err, runner.ErrNoValidDutiesToExecute):
				logMsg += ", dropping message and terminating committee-runner"
			case (errors.Is(err, runner.ErrNoRunningDuty) || errors.Is(err, runner.ErrInvalidPartialSigSlot) ||
				errors.Is(err, runner.ErrInstanceNotFound) || errors.Is(err, runner.ErrFutureMsg) ||
				errors.Is(err, runner.ErrWrongMsgHeight) || errors.Is(err, runner.ErrNoProposalForRound) ||
				errors.Is(err, runner.ErrWrongMsgRound) || errors.Is(err, runner.ErrNoDecidedValue)) && msgRetryCnt < retryCount:

				logMsg += fmt.Sprintf(", retrying message in ~%dms", retryDelay.Milliseconds())
				msgRetries.Set(messageID(msg), msgRetryCnt+1, ttlcache.DefaultTTL)
				go func() {
					time.Sleep(retryDelay)
					q.Q.Push(msg)
				}()
			default:
				logMsg += ", dropping message"
			}

			c.logMsg(
				logger,
				msg,
				logMsg,
				zap.String("message_identifier", string(messageID(msg))),
				fields.MessageType(msg.MsgType),
				zap.Error(err),
				zap.Int("attempt", msgRetryCnt+1),
			)

			if errors.Is(err, runner.ErrNoValidDutiesToExecute) {
				// Optimization: stop queue consumer if the runner no longer has any valid duties to execute.
				return
			}
		}
	}
}

func (c *Committee) logMsg(logger *zap.Logger, msg *queue.SSVMessage, logMsg string, withFields ...zap.Field) {
	baseFields := []zap.Field{}
	if msg.MsgType == spectypes.SSVConsensusMsgType {
		sm := msg.Body.(*specqbft.Message)
		baseFields = []zap.Field{
			zap.Uint64("msg_height", uint64(sm.Height)),
			zap.Uint64("msg_round", uint64(sm.Round)),
			zap.Uint64("consensus_msg_type", uint64(sm.MsgType)),
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

//
//// GetLastHeight returns the last height for the given identifier
//func (v *Committee) GetLastHeight(identifier spectypes.MessageID) specqbft.Height {
//	r := v.DutyRunners.DutyRunnerForMsgID(identifier)
//	if r == nil {
//		return specqbft.Height(0)
//	}
//	if ctrl := r.GetBaseRunner().QBFTController; ctrl != nil {
//		return ctrl.Height
//	}
//	return specqbft.Height(0)
//}
//
//// GetLastRound returns the last height for the given identifier
//func (v *Committee) GetLastRound(identifier spectypes.MessageID) specqbft.Round {
//	r := v.DutyRunners.DutyRunnerForMsgID(identifier)
//	if r == nil {
//		return specqbft.Round(1)
//	}
//	if r != nil && r.HasRunningDuty() {
//		inst := r.GetBaseRunner().State.RunningInstance
//		if inst != nil {
//			return inst.State.Round
//		}
//	}
//	return specqbft.Round(1)
//}
