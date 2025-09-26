package validator

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

// queueContainer wraps a queue with its corresponding state
type queueContainer struct {
	Q          queue.Queue
	queueState *queue.State
}

// EnqueueMessage enqueues a spectypes.SSVMessage for processing.
// TODO: accept DecodedSSVMessage once p2p is upgraded to decode messages during validation.
func (c *Committee) EnqueueMessage(ctx context.Context, msg *queue.SSVMessage) {
	msgType := msg.GetType()
	msgID := msg.GetID()

	logger := c.logger.
		With(fields.MessageType(msgType)).
		With(fields.MessageID(msgID)).
		With(fields.RunnerRole(msgID.GetRoleType()))

	slot, err := msg.Slot()
	if err != nil {
		logger.Error("❌ couldn't get message slot", zap.Error(err))
		return
	}
	dutyID := fields.BuildCommitteeDutyID(types.OperatorIDsFromOperators(c.CommitteeMember.Committee), c.networkConfig.EstimatedEpochAtSlot(slot), slot)

	logger = logger.
		With(fields.Slot(slot)).
		With(fields.DutyID(dutyID))

	ctx, span := tracer.Start(traces.Context(ctx, dutyID),
		observability.InstrumentName(observabilityNamespace, "enqueue_committee_message"),
		trace.WithAttributes(
			observability.ValidatorMsgTypeAttribute(msgType),
			observability.ValidatorMsgIDAttribute(msgID),
			observability.RunnerRoleAttribute(msgID.GetRoleType()),
			observability.CommitteeIDAttribute(c.CommitteeMember.CommitteeID),
			observability.BeaconSlotAttribute(slot),
			observability.DutyIDAttribute(dutyID)))
	defer span.End()

	msg.TraceContext = ctx

	c.mtx.Lock()
	q := c.getQueue(logger, slot)
	c.mtx.Unlock()

	span.AddEvent("pushing message to the queue")
	if pushed := q.Q.TryPush(msg); !pushed {
		const errMsg = "❗ dropping message because the queue is full"
		logger.Warn(errMsg)
		span.SetStatus(codes.Error, errMsg)
		return
	}

	span.SetStatus(codes.Ok, "")
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

	// queueCtx enforces a deadline for queue consumer to terminate and clean up resources at some point
	// in the future (when this queue becomes no longer relevant)
	queueCtx, cancelF := context.WithDeadline(ctx, c.networkConfig.EstimatedTimeAtSlot(duty.Slot+runnerExpirySlots))
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
	q queueContainer,
	logger *zap.Logger,
	handler MessageHandler,
	rnr *runner.CommitteeRunner,
) {
	logger.Debug("📬 queue consumer is running")
	defer logger.Debug("📪 queue consumer is closed")

	// Construct a representation of the current state.
	state := *q.queueState

	// msgRetries keeps track of how many times we've tried to handle a particular message. Since this map
	// grows over time, we need to clean it up automatically. There is no specific TTL value to use for its
	// entries - it just needs to be large enough to prevent unnecessary (but non-harmful) retries from happening.
	msgRetries := ttlcache.New(
		ttlcache.WithTTL[msgIDType, int](10 * time.Minute),
	)
	go msgRetries.Start()
	defer msgRetries.Stop()

	for ctx.Err() == nil {
		state.HasRunningInstance = rnr.HasRunningQBFTInstance()

		filter := queue.FilterAny
		if state.HasRunningInstance && !rnr.HasAcceptedProposalForCurrentRound() {
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
		} else if state.HasRunningInstance {
			filter = func(ssvMessage *queue.SSVMessage) bool {
				// don't read post consensus until decided
				return ssvMessage.MsgType != spectypes.SSVPartialSignatureMsgType
			}
		}

		// Pop the highest priority message for the current state.
		// TODO: (Alan) bring back filter
		msg := q.Q.Pop(ctx, queue.NewCommitteeQueuePrioritizer(&state), filter)
		if ctx.Err() != nil {
			// Optimization: terminate fast if we can.
			return
		}
		if msg == nil {
			logger.Error("❗ got nil message from queue, but context is not done!")
			return
		}

		// Handle the message, potentially scheduling a message-replay for later.
		err := handler(ctx, msg)
		if err != nil {
			// We'll re-queue the message to be replayed later in case the error we got is retryable.
			// We are aiming to cover most of the slot time (~12s), but we don't need to cover all 12s
			// since most duties must finish well before that anyway, and will take additional time
			// to execute as well.
			// Retry delay should be small so we can proceed with the corresponding duty execution asap.
			const (
				retryDelay = 25 * time.Millisecond
				retryCount = 40
			)
			msgRetryItem := msgRetries.Get(c.messageID(msg))
			if msgRetryItem == nil {
				msgRetries.Set(c.messageID(msg), 0, ttlcache.DefaultTTL)
				msgRetryItem = msgRetries.Get(c.messageID(msg))
			}
			msgRetryCnt := msgRetryItem.Value()

			logger = loggerWithMessageFields(logger, msg).
				With(zap.String("message_identifier", string(c.messageID(msg)))).
				With(zap.Int("attempt", msgRetryCnt+1))

			const couldNotHandleMsgLogPrefix = "could not handle message, "
			switch {
			case errors.Is(err, runner.ErrNoValidDutiesToExecute):
				logger.Error("❗ "+couldNotHandleMsgLogPrefix+"dropping message and terminating committee-runner", zap.Error(err))
			case errors.Is(err, &runner.RetryableError{}) && msgRetryCnt < retryCount:
				logger.Debug(fmt.Sprintf(couldNotHandleMsgLogPrefix+"retrying message in ~%dms", retryDelay.Milliseconds()), zap.Error(err))
				msgRetries.Set(c.messageID(msg), msgRetryCnt+1, ttlcache.DefaultTTL)
				go func(msg *queue.SSVMessage) {
					time.Sleep(retryDelay)
					if pushed := q.Q.TryPush(msg); !pushed {
						logger.Error("❗ not gonna replay message because the queue is full",
							zap.String("message_identifier", string(c.messageID(msg))),
							fields.MessageType(msg.MsgType),
						)
					}
				}(msg)
			default:
				logger.Warn(couldNotHandleMsgLogPrefix+"dropping message", zap.Error(err))
			}

			if errors.Is(err, runner.ErrNoValidDutiesToExecute) {
				// Optimization: stop queue consumer if the runner no longer has any valid duties to execute.
				return
			}
		}
	}
}

// messageID is a wrapper that provides a logger to report errors (if any).
func (c *Committee) messageID(msg *queue.SSVMessage) msgIDType {
	return messageID(msg, c.logger)
}
