package validator

import (
	"context"
	"fmt"
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
	"github.com/ssvlabs/ssv/observability/traces"
	"github.com/ssvlabs/ssv/protocol/v2/message"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

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
		if pushed := q.TryPush(msg); !pushed {
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

// StartQueueConsumer start consuming p2p message queue with the supplied handler
func (v *Validator) StartQueueConsumer(
	msgID spectypes.MessageID,
	handler MessageHandler, // should be v.ProcessMessage, it is a param so can be mocked out for testing
) {
	consumeQueue := func(ctx context.Context) error {
		var q queue.Queue
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
		defer v.logger.Debug("📪 queue consumer is closed")

		// msgRetries keeps track of how many times we've tried to handle a particular message. Since this map
		// grows over time, we need to clean it up automatically. There is no specific TTL value to use for its
		// entries - it just needs to be large enough to prevent unnecessary (but non-harmful) retries from happening.
		msgRetries := ttlcache.New(
			ttlcache.WithTTL[msgIDType, int](10 * time.Minute),
		)
		go msgRetries.Start()
		defer msgRetries.Stop()

		for ctx.Err() == nil {
			// Construct a representation of the current state.
			state := queue.State{}
			r := v.DutyRunners.DutyRunnerForMsgID(msgID)
			if r == nil {
				return fmt.Errorf("could not get duty runner for msg ID %v", msgID)
			}
			state.HasRunningInstance = r.HasRunningQBFTInstance()
			state.Height = r.GetLastHeight()
			state.Round = r.GetLastRound()
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
			} else if state.HasRunningInstance && !r.HasAcceptedProposalForCurrentRound() {
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
			msg := q.Pop(ctx, queue.NewMessagePrioritizer(&state), filter)
			if ctx.Err() != nil {
				// Optimization: terminate fast if we can.
				return nil
			}
			if msg == nil {
				v.logger.Error("❗ got nil message from queue, but context is not done!")
				return nil
			}

			msgLogger, err := v.logWithMessageFields(v.logger, msg)
			if err != nil {
				v.logger.Error("couldn't build message-logger, dropping message", zap.Error(err))
				continue
			}

			// Handle the message, potentially scheduling a message-replay for later.
			err = handler(ctx, msgLogger, msg)
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
				msgRetryItem := msgRetries.Get(v.messageID(msg))
				if msgRetryItem == nil {
					msgRetries.Set(v.messageID(msg), 0, ttlcache.DefaultTTL)
					msgRetryItem = msgRetries.Get(v.messageID(msg))
				}
				msgRetryCnt := msgRetryItem.Value()

				msgLogger = logWithMessageMetadata(msgLogger, msg).
					With(zap.String("message_identifier", string(v.messageID(msg)))).
					With(zap.Int("attempt", msgRetryCnt+1))

				const couldNotHandleMsgLogPrefix = "could not handle message, "
				switch {
				case errors.Is(err, &runner.RetryableError{}) && msgRetryCnt < retryCount:
					msgLogger.Debug(fmt.Sprintf(couldNotHandleMsgLogPrefix+"retrying message in ~%dms", retryDelay.Milliseconds()), zap.Error(err))
					msgRetries.Set(v.messageID(msg), msgRetryCnt+1, ttlcache.DefaultTTL)
					go func(msg *queue.SSVMessage) {
						time.Sleep(retryDelay)
						if pushed := q.TryPush(msg); !pushed {
							msgLogger.Warn("❗ not gonna replay message because the queue is full",
								zap.String("message_identifier", string(v.messageID(msg))),
								fields.MessageType(msg.MsgType),
							)
						}
					}(msg)
				default:
					msgLogger.Warn(couldNotHandleMsgLogPrefix+"dropping message", zap.Error(err))
				}
			}
		}

		return nil
	}

	go func() {
		for v.ctx.Err() == nil {
			err := consumeQueue(v.ctx)
			if err != nil {
				v.logger.Debug("❗ failed consuming queue", zap.Error(err))
			}
		}
	}()
}

// ProcessMessage processes p2p message of all types
func (v *Validator) ProcessMessage(ctx context.Context, logger *zap.Logger, msg *queue.SSVMessage) error {
	msgType := msg.GetType()
	msgID := msg.GetID()

	// Validate message (+ verify SignedSSVMessage's signature)
	if msgType != message.SSVEventMsgType {
		if err := msg.SignedSSVMessage.Validate(); err != nil {
			return fmt.Errorf("invalid SignedSSVMessage: %w", err)
		}
		if err := spectypes.Verify(msg.SignedSSVMessage, v.Operator.Committee); err != nil {
			return fmt.Errorf("SignedSSVMessage has an invalid signature: %w", err)
		}
	}

	slot, err := msg.Slot()
	if err != nil {
		return fmt.Errorf("couldn't get message slot: %w", err)
	}
	dutyID := fields.BuildDutyID(v.NetworkConfig.EstimatedEpochAtSlot(slot), slot, msgID.GetRoleType(), v.Share.ValidatorIndex)

	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "process_message"),
		trace.WithAttributes(
			observability.ValidatorMsgTypeAttribute(msgType),
			observability.ValidatorMsgIDAttribute(msgID),
			observability.RunnerRoleAttribute(msgID.GetRoleType()),
			observability.BeaconSlotAttribute(slot),
			observability.DutyIDAttribute(dutyID),
		),
		trace.WithLinks(trace.LinkFromContext(msg.TraceContext)))
	defer span.End()

	// Get runner
	dutyRunner := v.DutyRunners.DutyRunnerForMsgID(msgID)
	if dutyRunner == nil {
		return traces.Errorf(span, "could not get duty runner for msg ID %v", msgID)
	}

	// Validate message for runner
	if err := validateMessage(v.Share.Share, msg); err != nil {
		return traces.Errorf(span, "message invalid for msg ID %v: %w", msgID, err)
	}
	switch msgType {
	case spectypes.SSVConsensusMsgType:
		qbftMsg, ok := msg.Body.(*specqbft.Message)
		if !ok {
			return traces.Errorf(span, "could not decode consensus message from network message")
		}

		if err := qbftMsg.Validate(); err != nil {
			return traces.Errorf(span, "invalid QBFT Message: %w", err)
		}

		if err := dutyRunner.ProcessConsensus(ctx, logger, msg.SignedSSVMessage); err != nil {
			return traces.Error(span, err)
		}
		span.SetStatus(codes.Ok, "")
		return nil
	case spectypes.SSVPartialSignatureMsgType:
		signedMsg, ok := msg.Body.(*spectypes.PartialSignatureMessages)
		if !ok {
			return traces.Errorf(span, "could not decode post consensus message from network message")
		}

		span.SetAttributes(observability.ValidatorPartialSigMsgTypeAttribute(signedMsg.Type))

		if len(msg.SignedSSVMessage.OperatorIDs) != 1 {
			return traces.Errorf(span, "PartialSignatureMessage has more than 1 signer")
		}

		if err := signedMsg.ValidateForSigner(msg.SignedSSVMessage.OperatorIDs[0]); err != nil {
			return traces.Errorf(span, "invalid PartialSignatureMessages: %w", err)
		}

		if signedMsg.Type == spectypes.PostConsensusPartialSig {
			span.AddEvent("processing post-consensus message")
			if err := dutyRunner.ProcessPostConsensus(ctx, logger, signedMsg); err != nil {
				return traces.Error(span, err)
			}
			span.SetStatus(codes.Ok, "")
			return nil
		}
		span.AddEvent("processing pre-consensus message")
		if err := dutyRunner.ProcessPreConsensus(ctx, logger, signedMsg); err != nil {
			return traces.Error(span, err)
		}
		span.SetStatus(codes.Ok, "")
		return nil
	case message.SSVEventMsgType:
		if err := v.handleEventMessage(ctx, logger, msg, dutyRunner); err != nil {
			return traces.Errorf(span, "could not handle event message: %w", err)
		}
		span.SetStatus(codes.Ok, "")
		return nil
	default:
		return traces.Errorf(span, "unknown message type %d", msgType)
	}
}

func validateMessage(share spectypes.Share, msg *queue.SSVMessage) error {
	if !share.ValidatorPubKey.MessageIDBelongs(msg.GetID()) {
		return errors.New("msg ID doesn't match validator ID")
	}

	if len(msg.GetData()) == 0 {
		return errors.New("msg data is invalid")
	}

	return nil
}

func (v *Validator) logWithMessageFields(logger *zap.Logger, msg *queue.SSVMessage) (*zap.Logger, error) {
	msgType := msg.GetType()
	msgID := msg.GetID()

	slot, err := msg.Slot()
	if err != nil {
		return nil, fmt.Errorf("couldn't get message slot: %w", err)
	}
	dutyID := fields.BuildDutyID(v.NetworkConfig.EstimatedEpochAtSlot(slot), slot, msgID.GetRoleType(), v.Share.ValidatorIndex)

	logger = logger.
		With(fields.MessageType(msgType)).
		With(fields.RunnerRole(msgID.GetRoleType())).
		With(fields.Slot(slot)).
		With(fields.DutyID(dutyID)).
		With(fields.EstimatedCurrentEpoch(v.NetworkConfig.EstimatedCurrentEpoch())).
		With(fields.EstimatedCurrentSlot(v.NetworkConfig.EstimatedCurrentSlot()))

	if msg.MsgType == spectypes.SSVConsensusMsgType {
		qbftMsg := msg.Body.(*specqbft.Message)
		logger = logger.With(fields.QBFTRound(qbftMsg.Round), fields.QBFTHeight(qbftMsg.Height))
	}

	return logger, nil
}

// messageID is a wrapper that provides a logger to report errors (if any).
func (v *Validator) messageID(msg *queue.SSVMessage) msgIDType {
	return messageID(msg, v.logger)
}
