package validator

import (
	"context"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
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

	_, span := tracer.Start(traces.Context(ctx, dutyID),
		observability.InstrumentName(observabilityNamespace, "enqueue_validator_message"),
		trace.WithAttributes(
			observability.ValidatorMsgTypeAttribute(msgType),
			observability.ValidatorMsgIDAttribute(msgID),
			observability.RunnerRoleAttribute(msgID.GetRoleType()),
			observability.BeaconSlotAttribute(slot),
			observability.DutyIDAttribute(dutyID)))
	defer span.End()

	v.mtx.RLock() // read v.Queues
	defer v.mtx.RUnlock()
	if q, ok := v.Queues[msg.MsgID.GetRoleType()]; ok {
		span.AddEvent("pushing message to queue")
		if pushed := q.TryPush(msg); !pushed {
			const eventMsg = "❗ dropping message because the queue is full"
			logger.Warn(eventMsg,
				zap.String("drop_reason", queue.DropReasonBufferFull),
				zap.String("msg_type", message.MsgTypeToString(msg.MsgType)),
				zap.String("msg_id", msg.MsgID.String()))

			span.AddEvent(eventMsg, trace.WithAttributes(attribute.String("drop_reason", queue.DropReasonBufferFull)))
			span.SetStatus(codes.Error, eventMsg)
			return
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
				return fmt.Errorf("queue not found for role %s", types.RunnerRoleToString(msgID.GetRoleType()))
			}
			return nil
		}()
		if err != nil {
			return err
		}

		v.logger.Debug("📬 queue consumer is running")
		defer v.logger.Debug("📪 queue consumer is closed")

		// msgStates keeps track of in-flight processing state (retry count + span context) per message.
		msgStates := newMessageStates(messageStateTTL)
		go msgStates.Start()
		defer msgStates.Stop()

		// rState defines current runner state that will be used for deciding which messages we want to process
		// sooner (vs which ones can wait till later).
		rState := queue.State{
			Quorum: v.Operator.GetQuorum(), // never changes for duty runner
		}

		// floor is the slot of the duty the runner is currently serving; anything queued below it is stale
		// (a validator runner advances through its duties in slot order). It is set at each idle duty start
		// and read only here, in this single consumer goroutine.
		var floor phase0.Slot

		for ctx.Err() == nil {
			r := v.DutyRunners.DutyRunnerForMsgID(msgID)
			if r == nil {
				return fmt.Errorf("could not get duty runner for msg ID %v", msgID)
			}

			// Update rState to incorporate the effects that the previously handled message might have
			// had on the runner state.
			rState.HasRunningInstance = r.HasRunningQBFTInstance()
			rState.Slot = phase0.Slot(r.GetLastHeight())
			rState.Round = r.GetLastRound()

			idle := !r.HasRunningDuty()
			filter := queue.FilterAny
			if idle {
				// If no duty is running, pop only ExecuteDuty messages.
				filter = func(m *queue.SSVMessage) bool {
					e, ok := m.Body.(*types.EventMsg)
					if !ok || e == nil {
						return false
					}
					return e.Type == types.ExecuteDuty
				}
			} else if rState.HasRunningInstance && !r.HasAcceptedProposalForCurrentRound() {
				// If no proposal was accepted for the current round, skip prepare & commit messages
				// for the current height and round.
				filter = func(m *queue.SSVMessage) bool {
					qbftMsg, ok := m.Body.(*specqbft.Message)
					if !ok || qbftMsg == nil {
						return true
					}

					if qbftMsg.Height != specqbft.Height(rState.Slot) || qbftMsg.Round != rState.Round {
						return true
					}
					return qbftMsg.MsgType != specqbft.PrepareMsgType && qbftMsg.MsgType != specqbft.CommitMsgType
				}
			}

			// Pop the highest priority message for the current state.
			msg := q.Pop(ctx, queue.NewMessagePrioritizer(&rState), filter)
			if ctx.Err() != nil {
				// Optimization: terminate fast if we can.
				return nil
			}
			if msg == nil {
				v.logger.Error("❗ got nil message from queue, but context is not done!")
				return nil
			}

			// An idle runner starting a duty raises its slot floor: the stale tail still queued for an earlier
			// slot — leftovers of the concluded duty, or messages for a duty this operator never ran — can no
			// longer be processed, and would otherwise be handed to the new duty one by one only to be rejected
			// (issue #3037). Purge it before the duty runs (slotBelow spares duty-starts, so none is skipped),
			// closing out any in-flight state each purged message still holds from an earlier attempt.
			if dutySlot, ok := executeDutySlot(msg); ok && idle {
				floor = dutySlot
				dropped := q.Purge(slotBelow(dutySlot), queue.PurgeReasonStale, func(removed *queue.SSVMessage) {
					endStaleMessageState(msgStates, v.logger, removed)
				})
				if dropped > 0 {
					v.logger.Debug("dropped stale messages queued for slots before the starting duty",
						fields.RunnerRole(msgID.GetRoleType()), fields.Slot(dutySlot), fields.Count(dropped))
				}
			}

			// A message can still sit below the floor after the bulk purge — a straggler re-pushed by a
			// retry goroutine that was parked during the purge, or one that simply arrived later. Handing
			// it to the runner would draw the exact "invalid partial sig slot" rejection the purge prevents
			// (issue #3037), so drop it here too. slotBelow spares duty-starts and matches nothing at floor 0.
			if slotBelow(floor)(msg) {
				endStaleMessageState(msgStates, v.logger, msg)
				q.RecordPurge(queue.PurgeReasonStale)
				v.logger.Debug("dropped a stale message that reached the consumer after the purge",
					fields.RunnerRole(msgID.GetRoleType()), fields.Slot(floor))
				continue
			}

			msgLogger, err := v.logWithMessageFields(v.logger, msg)
			if err != nil {
				v.logger.Error("couldn't build message-logger, dropping message", zap.Error(err))
				continue
			}

			msgKey, err := mKey(msg)
			if err != nil {
				v.logger.Error("couldn't build msgKey, dropping message", zap.Error(err))
				continue
			}

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
				)}

				slot, slotErr := msg.Slot()
				if slotErr == nil {
					dutyID := fields.BuildDutyID(v.NetworkConfig.EstimatedEpochAtSlot(slot), slot, msgID.GetRoleType(), v.Share.ValidatorIndex)
					spanOpts = append(spanOpts, trace.WithAttributes(
						observability.BeaconSlotAttribute(slot),
						observability.DutyIDAttribute(dutyID),
					))
					msgCtx = traces.Context(msgCtx, dutyID)
				} else {
					msgLogger.Warn("couldn't get message slot for tracing metadata", zap.Error(slotErr))
				}

				msgCtx, msgSpan := tracer.Start(msgCtx,
					observability.InstrumentName(observabilityNamespace, "process_validator_message"),
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
				// We are aiming to cover most of the slot time (~10s), but we don't need to cover the
				// full slot (all 12s) since most duties must finish well before that anyway, and will
				// take additional time to execute as well.
				// Retry delay should be small so we can proceed with the corresponding duty execution asap.
				const retryDelay = 25 * time.Millisecond
				retryCount := int64(v.NetworkConfig.SlotDuration / retryDelay)

				msgLogger = logWithMessageMetadata(msgLogger, msg).
					With(zap.String("message_key", string(msgKey))).
					With(zap.Int64("attempt", currentAttempt))

				const couldNotHandleMsgLogPrefix = "could not handle message, "
				switch {
				case runner.IsRetryable(err) && msgState.attempts <= retryCount:
					msgState.attempts++
					msgStates.Set(msgKey, msgState, ttlcache.DefaultTTL)
					msgState.span.AddEvent(fmt.Sprintf(couldNotHandleMsgLogPrefix+"retrying in ~%dms", retryDelay.Milliseconds()),
						trace.WithAttributes(
							attribute.String("retry_reason", err.Error()),
							attribute.Int64("attempt", currentAttempt),
						),
					)
					go func(msg *queue.SSVMessage, msgState *messageProcessingState, attempt int64) {
						select {
						case <-time.After(retryDelay):
						case <-msgState.ctx.Done():
							return
						}
						if pushed := q.TryPush(msg); !pushed {
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
					msgLogger.Debug(droppingMsgDueToErrorEvent, zap.Error(err))
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
		qbftMsg, ok := msg.Body.(*specqbft.Message)
		if !ok || qbftMsg == nil {
			return nil, fmt.Errorf("invalid qbft msg body, type: %T", msg.Body)
		}
		logger = logger.With(fields.QBFTRound(qbftMsg.Round), fields.QBFTHeight(qbftMsg.Height))
	}
	if msg.MsgType == message.SSVEventMsgType {
		eventMsg, ok := msg.Body.(*types.EventMsg)
		if !ok || eventMsg == nil {
			return nil, fmt.Errorf("event message: invalid msg body, type: %T", msg.Body)
		}
		if eventMsg.Type == types.Timeout {
			timeoutData, err := eventMsg.GetTimeoutData()
			if err != nil {
				return nil, fmt.Errorf("event message: get timeout data: %w", err)
			}
			logger = logger.With(fields.QBFTRound(timeoutData.Round))
		}
	}

	return logger, nil
}

// isExecuteDuty reports whether msg is a duty-start event.
func isExecuteDuty(msg *queue.SSVMessage) bool {
	event, ok := msg.Body.(*types.EventMsg)
	return ok && event != nil && event.Type == types.ExecuteDuty
}

// executeDutySlot returns the slot of the duty a duty-start event carries, if msg is one.
func executeDutySlot(msg *queue.SSVMessage) (phase0.Slot, bool) {
	if !isExecuteDuty(msg) {
		return 0, false
	}
	slot, err := msg.Slot()
	return slot, err == nil
}

// slotBelow matches messages stranded below floor. A validator runner moves through its duties in slot
// order and never returns to a lower slot, so once the duty at floor starts, nothing below it has a duty
// left to serve it.
//
// Duty-start events are the exception and are never matched: dropping one would silently skip a duty.
// Duties are enqueued from racing per-duty goroutines (scheduler.executeDuties), so a higher-slot
// duty-start can be popped while a lower-slot one still waits in the queue — and that one must survive.
func slotBelow(floor phase0.Slot) queue.Filter {
	return func(m *queue.SSVMessage) bool {
		if isExecuteDuty(m) {
			return false
		}
		slot, err := m.Slot()
		return err == nil && slot < floor
	}
}
