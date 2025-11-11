package validator

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jellydator/ttlcache/v3"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/attribute"
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

	// msgStates keeps track of in-flight processing state (retry count + span context) per message.
	// Since this map grows over time, we need to clean it up automatically. There is no specific TTL value
	// to use for its entries - it just needs to be large enough to prevent unnecessary (but non-harmful)
	// retries from happening.
	msgStates := ttlcache.New(
		ttlcache.WithTTL[messageKey, *messageProcessingState](10 * time.Minute),
	)
	go msgStates.Start()
	defer msgStates.Stop()

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

		msgKey := c.mKey(msg)

		var msgState *messageProcessingState
		msgStateItem := msgStates.Get(msgKey)
		if msgStateItem != nil {
			msgState = msgStateItem.Value()
		}
		if msgState == nil {
			msgCtx := ctx

			spanOpts := []trace.SpanStartOption{trace.WithAttributes(
				observability.ValidatorMsgTypeAttribute(msg.GetType()),
				observability.ValidatorMsgIDAttribute(msg.GetID()),
				observability.RunnerRoleAttribute(msg.GetID().GetRoleType()),
				observability.CommitteeIDAttribute(c.CommitteeMember.CommitteeID),
			)}

			slot, slotErr := msg.Slot()
			if slotErr == nil {
				dutyID := fields.BuildCommitteeDutyID(
					types.OperatorIDsFromOperators(c.CommitteeMember.Committee),
					c.networkConfig.EstimatedEpochAtSlot(slot),
					slot,
				)
				spanOpts = append(spanOpts, trace.WithAttributes(
					observability.BeaconSlotAttribute(slot),
					observability.DutyIDAttribute(dutyID),
				))
				msgCtx = traces.Context(msgCtx, dutyID)
			} else {
				msgLogger.Warn("couldn't get message slot for tracing metadata", zap.Error(slotErr))
			}

			msgCtx, msgSpan := tracer.Start(msgCtx,
				observability.InstrumentName(observabilityNamespace, "process_committee_message"),
				spanOpts...,
			)
			msgState = &messageProcessingState{
				attempts: 0,
				ctx:      msgCtx,
				span:     msgSpan,
			}
			msgStates.Set(msgKey, msgState, ttlcache.DefaultTTL)
		}

		currentAttempt := msgState.attempts + 1
		msgState.span.AddEvent("dequeued message for processing", trace.WithAttributes(
			attribute.Int64("attempt", currentAttempt),
		))

		// Handle the message, potentially scheduling a message-replay for later.
		err = handler(msgState.ctx, msgLogger, msg)
		if err != nil {
			// We'll re-queue the message to be replayed later in case the error we got is retryable.
			// We are aiming to cover most of the slot time (~12s), but we don't need to cover the
			// full slot (all 12s) since most duties must finish well before that anyway, and will
			// take additional time to execute as well.
			// Retry delay should be small so we can proceed with the corresponding duty execution asap.
			const retryDelay = 25 * time.Millisecond
			retryCount := int64(c.networkConfig.SlotDuration / retryDelay)

			msgLogger = logWithMessageMetadata(msgLogger, msg).
				With(zap.String("message_key", string(msgKey))).
				With(zap.Int64("attempt", currentAttempt))

			const couldNotHandleMsgLogPrefix = "could not handle message, "
			switch {
			case errors.Is(err, runner.ErrNoValidDutiesToExecute):
				const droppingMsgDueToNoValidDutiesToExecuteEvent = "❗ " + couldNotHandleMsgLogPrefix + "dropping message and terminating committee-runner"
				msgLogger.Error(droppingMsgDueToNoValidDutiesToExecuteEvent, zap.Error(err))
				msgState.span.AddEvent(droppingMsgDueToNoValidDutiesToExecuteEvent, trace.WithAttributes(
					attribute.String("drop_reason", err.Error()),
					attribute.Int64("attempt", currentAttempt),
				))
				msgState.span.SetStatus(codes.Error, droppingMsgDueToNoValidDutiesToExecuteEvent)
				msgState.span.End()
				msgStates.Delete(msgKey)
				return
			case runner.IsRetryable(err) && msgState.attempts < retryCount:
				msgState.attempts++
				msgStates.Set(msgKey, msgState, ttlcache.DefaultTTL)
				var retryingMsgDueToErrorEvent = fmt.Sprintf(couldNotHandleMsgLogPrefix+"retrying message in ~%dms", retryDelay.Milliseconds())
				msgLogger.Debug(retryingMsgDueToErrorEvent, zap.Error(err))
				msgState.span.AddEvent(retryingMsgDueToErrorEvent, trace.WithAttributes(
					attribute.String("retry_reason", err.Error()),
					attribute.Int64("attempt", currentAttempt),
				))
				go func(msg *queue.SSVMessage, msgState *messageProcessingState, attempt int64) {
					select {
					case <-time.After(retryDelay):
					case <-msgState.ctx.Done():
						return
					}
					if pushed := q.Q.TryPush(msg); !pushed {
						const droppingMsgDueToQueueIsFullEvent = "❗ not gonna replay message because the queue is full"
						msgLogger.Error(droppingMsgDueToQueueIsFullEvent)
						msgState.span.AddEvent(droppingMsgDueToQueueIsFullEvent, trace.WithAttributes(
							attribute.Int64("attempt", attempt),
						))
						msgState.span.SetStatus(codes.Error, droppingMsgDueToQueueIsFullEvent)
						msgState.span.End()
						msgStates.Delete(msgKey)
					}
				}(msg, msgState, currentAttempt)
			default:
				var droppingMsgDueToErrorEvent = couldNotHandleMsgLogPrefix + "dropping message"
				msgLogger.Warn(droppingMsgDueToErrorEvent, zap.Error(err))
				msgState.span.AddEvent(droppingMsgDueToErrorEvent, trace.WithAttributes(
					attribute.String("drop_reason", err.Error()),
					attribute.Int64("attempt", currentAttempt),
				))
				msgState.span.SetStatus(codes.Error, droppingMsgDueToErrorEvent)
				msgState.span.End()
				msgStates.Delete(msgKey)
			}
		} else {
			msgState.span.AddEvent("message processed successfully", trace.WithAttributes(
				attribute.Int64("attempt", currentAttempt),
			))
			msgState.span.SetStatus(codes.Ok, "")
			msgState.span.End()
			msgStates.Delete(msgKey)
		}
	}
}

// ProcessMessage processes p2p message of all types
func (c *Committee) ProcessMessage(ctx context.Context, logger *zap.Logger, msg *queue.SSVMessage) error {
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	span.AddEvent("got committee message to process")

	msgType := msg.GetType()

	// Validate message (+ verify SignedSSVMessage's signature)
	if msgType != message.SSVEventMsgType {
		if err := msg.SignedSSVMessage.Validate(); err != nil {
			return fmt.Errorf("invalid SignedSSVMessage: %w", err)
		}
		if err := spectypes.Verify(msg.SignedSSVMessage, c.CommitteeMember.Committee); err != nil {
			return spectypes.WrapError(spectypes.SSVMessageHasInvalidSignatureErrorCode, fmt.Errorf("SignedSSVMessage has an invalid signature: %w", err))
		}
		if err := c.validateMessage(msg.SignedSSVMessage.SSVMessage); err != nil {
			// TODO - we should improve this error message as is suggested by the commented-out code here
			// (and also remove nolint annotation), currently we cannot do it due to spec-tests expecting
			// this exact format we are stuck with.
			//return fmt.Errorf("SSVMessage invalid: %w", err)
			return fmt.Errorf("Message invalid: %w", err) //nolint:staticcheck
		}
	}

	slot, err := msg.Slot()
	if err != nil {
		return fmt.Errorf("couldn't get message slot: %w", err)
	}

	switch msgType {
	case spectypes.SSVConsensusMsgType:
		span.AddEvent("process committee message = consensus message")

		qbftMsg := &specqbft.Message{}
		if err := qbftMsg.Decode(msg.GetData()); err != nil {
			return fmt.Errorf("could not decode consensus Message: %w", err)
		}
		if err := qbftMsg.Validate(); err != nil {
			return fmt.Errorf("invalid QBFT Message: %w", err)
		}
		c.mtx.RLock()
		r, exists := c.Runners[slot]
		c.mtx.RUnlock()
		if !exists {
			return spectypes.WrapError(spectypes.NoRunnerForSlotErrorCode, fmt.Errorf("no runner found for message's slot"))
		}
		return r.ProcessConsensus(ctx, logger, msg.SignedSSVMessage)
	case spectypes.SSVPartialSignatureMsgType:
		pSigMessages := &spectypes.PartialSignatureMessages{}
		if err := pSigMessages.Decode(msg.SignedSSVMessage.SSVMessage.GetData()); err != nil {
			return fmt.Errorf("could not decode PartialSignatureMessages: %w", err)
		}

		// Validate
		if len(msg.SignedSSVMessage.OperatorIDs) != 1 {
			return fmt.Errorf("PartialSignatureMessage has more than 1 signer")
		}

		if err := pSigMessages.ValidateForSigner(msg.SignedSSVMessage.OperatorIDs[0]); err != nil {
			return fmt.Errorf("invalid PartialSignatureMessages: %w", err)
		}

		if pSigMessages.Type == spectypes.PostConsensusPartialSig {
			span.AddEvent("process committee message = post-consensus message")

			c.mtx.RLock()
			r, exists := c.Runners[pSigMessages.Slot]
			c.mtx.RUnlock()
			if !exists {
				return spectypes.WrapError(spectypes.NoRunnerForSlotErrorCode, fmt.Errorf("no runner found for message's slot"))
			}
			if err := r.ProcessPostConsensus(ctx, logger, pSigMessages); err != nil {
				return err
			}
			return nil
		}
	case message.SSVEventMsgType:
		span.AddEvent("process committee message = event message")

		if err := c.handleEventMessage(ctx, logger, msg); err != nil {
			return fmt.Errorf("could not handle event message: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unknown message type: %d", msgType)
	}

	return nil
}

func (c *Committee) logWithMessageFields(logger *zap.Logger, msg *queue.SSVMessage) (*zap.Logger, error) {
	msgType := msg.GetType()

	logger = logger.With(fields.MessageType(msgType))

	if msg.MsgType == spectypes.SSVConsensusMsgType {
		qbftMsg := msg.Body.(*specqbft.Message)
		logger = logger.With(fields.QBFTRound(qbftMsg.Round), fields.QBFTHeight(qbftMsg.Height))
	}

	return logger, nil
}

// mKey is a wrapper that provides a logger to report errors (if any).
func (c *Committee) mKey(msg *queue.SSVMessage) messageKey {
	return mKey(msg, c.logger)
}
