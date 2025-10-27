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
	"github.com/ssvlabs/ssv/protocol/v2/message"
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

	_, span := tracer.Start(traces.Context(ctx, dutyID),
		observability.InstrumentName(observabilityNamespace, "enqueue_committee_message"),
		trace.WithAttributes(
			observability.ValidatorMsgTypeAttribute(msgType),
			observability.ValidatorMsgIDAttribute(msgID),
			observability.RunnerRoleAttribute(msgID.GetRoleType()),
			observability.CommitteeIDAttribute(c.CommitteeMember.CommitteeID),
			observability.BeaconSlotAttribute(slot),
			observability.DutyIDAttribute(dutyID)))
	defer span.End()

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

// ConsumeQueue consumes messages from the queue.Queue of the controller
// it checks for current state
func (c *Committee) ConsumeQueue(
	ctx context.Context,
	logger *zap.Logger,
	q queueContainer,
	handler MessageHandler, // should be c.ProcessMessage, it is a param so can be mocked out for testing
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
		ttlcache.WithTTL[msgIDType, int64](10 * time.Minute),
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

		msgLogger, err := c.logWithMessageFields(logger, msg)
		if err != nil {
			logger.Error("couldn't build message-logger, dropping message", zap.Error(err))
			continue
		}

		// Handle the message, potentially scheduling a message-replay for later.
		err = handler(ctx, msgLogger, msg)
		if err != nil {
			// We'll re-queue the message to be replayed later in case the error we got is retryable.
			// We are aiming to cover most of the slot time (~12s), but we don't need to cover the
			// full slot (all 12s) since most duties must finish well before that anyway, and will
			// take additional time to execute as well.
			// Retry delay should be small so we can proceed with the corresponding duty execution asap.
			const retryDelay = 25 * time.Millisecond
			retryCount := int64(c.networkConfig.SlotDuration / retryDelay)

			msgRetryItem := msgRetries.Get(c.messageID(msg))
			if msgRetryItem == nil {
				msgRetries.Set(c.messageID(msg), 0, ttlcache.DefaultTTL)
				msgRetryItem = msgRetries.Get(c.messageID(msg))
			}
			msgRetryCnt := msgRetryItem.Value()

			msgLogger = logWithMessageMetadata(msgLogger, msg).
				With(zap.String("message_identifier", string(c.messageID(msg)))).
				With(zap.Int64("attempt", msgRetryCnt+1))

			const couldNotHandleMsgLogPrefix = "could not handle message, "
			switch {
			case errors.Is(err, runner.ErrNoValidDutiesToExecute):
				msgLogger.Error("❗ "+couldNotHandleMsgLogPrefix+"dropping message and terminating committee-runner", zap.Error(err))
			case runner.IsRetryable(err) && msgRetryCnt < retryCount:
				msgLogger.Debug(fmt.Sprintf(couldNotHandleMsgLogPrefix+"retrying message in ~%dms", retryDelay.Milliseconds()), zap.Error(err))
				msgRetries.Set(c.messageID(msg), msgRetryCnt+1, ttlcache.DefaultTTL)
				go func(msg *queue.SSVMessage) {
					time.Sleep(retryDelay)
					if pushed := q.Q.TryPush(msg); !pushed {
						msgLogger.Error("❗ not gonna replay message because the queue is full",
							zap.String("message_identifier", string(c.messageID(msg))),
							fields.MessageType(msg.MsgType),
						)
					}
				}(msg)
			default:
				msgLogger.Warn(couldNotHandleMsgLogPrefix+"dropping message", zap.Error(err))
			}

			if errors.Is(err, runner.ErrNoValidDutiesToExecute) {
				// Optimization: stop queue consumer if the runner no longer has any valid duties to execute.
				return
			}
		}
	}
}

func (c *Committee) logWithMessageFields(logger *zap.Logger, msg *queue.SSVMessage) (*zap.Logger, error) {
	msgType := msg.GetType()

	logger = logger.With(fields.MessageType(msgType))

	if msg.MsgType == spectypes.SSVConsensusMsgType {
		qbftMsg := msg.Body.(*specqbft.Message)
		logger = logger.With(fields.QBFTRound(qbftMsg.Round), fields.QBFTHeight(qbftMsg.Height))
	}
	if msg.MsgType == message.SSVEventMsgType {
		eventMsg, ok := msg.Body.(*types.EventMsg)
		if !ok {
			return nil, fmt.Errorf("could not decode event message")
		}
		if eventMsg.Type == types.Timeout {
			timeoutData, err := eventMsg.GetTimeoutData()
			if err != nil {
				return nil, fmt.Errorf("get timeout data: %w", err)
			}
			logger = logger.With(fields.QBFTRound(timeoutData.Round), fields.QBFTHeight(timeoutData.Height))
		}
	}

	return logger, nil
}

// messageID is a wrapper that provides a logger to report errors (if any).
func (c *Committee) messageID(msg *queue.SSVMessage) msgIDType {
	return messageID(msg, c.logger)
}
