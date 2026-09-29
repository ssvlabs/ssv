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

		// floor is the slot of the duty the runner is currently serving; anything queued below it is stale and
		// must not reach the runner. It is synced to the runner's own current-duty slot right after each
		// duty-start is handled — the runner, not the popped event, is authoritative on which duty it accepted
		// (it may accept a lower slot, or reject the start) — and is written and read only here, in this single
		// consumer goroutine.
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
			// A runner serving several slots at once is outside the consumer's single-duty notions: the idle
			// hold below (one slot's duty concluding says nothing about the others'), and the stale floor,
			// which has no current slot to key on and so stays at zero for it.
			_, multiSlot := r.(runner.MultiSlotRunner)

			// Pop the highest priority message the runner can use in its current state.
			msg := q.Pop(ctx, queue.NewMessagePrioritizer(&rState), popFilter(r, idle, multiSlot, &rState))
			if ctx.Err() != nil {
				// Optimization: terminate fast if we can.
				return nil
			}
			if msg == nil {
				v.logger.Error("❗ got nil message from queue, but context is not done!")
				return nil
			}

			// A message can still sit below the floor when it reaches the consumer — a straggler re-pushed by a
			// retry goroutine, one that arrived late, or the tail of a duty the runner has since moved past (a
			// mid-duty re-seat skips the bulk purge). Handing any of them to the runner would draw the exact
			// "invalid partial sig slot" rejection the floor prevents (issue #3037), so drop it here. slotBelow
			// spares duty-starts and matches nothing at floor 0.
			if slotBelow(floor)(msg) {
				endStaleMessageState(msgStates, v.logger, msg)
				q.RecordPurge(queue.PurgeReasonStale)
				v.logger.Debug("dropped a stale message that reached the consumer below the slot floor",
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

			// A duty-start may have moved the runner onto a new duty. Sync the floor to the slot the runner
			// actually accepted — the runner is authoritative here, not the popped event: while its instance
			// height is still 0 it accepts a duty for any slot, so it can re-seat to a slot below the floor, and
			// it can reject the start outright; either way the popped slot can't be trusted. Everything queued
			// below the new floor is stale (issue #3037). If the runner had been idle, bulk-purge that tail up
			// front (slotBelow spares duty-starts, so none is skipped), closing out each purged message's
			// in-flight state; mid-duty the pop-guard above drains it one at a time, sparing the busy hot path.
			if isExecuteDuty(msg) && !multiSlot {
				if dutySlot, ok := r.CurrentDutySlot(); ok {
					floor = dutySlot
					if idle {
						dropped := q.Purge(slotBelow(floor), queue.PurgeReasonStale, func(removed *queue.SSVMessage) {
							endStaleMessageState(msgStates, v.logger, removed)
						})
						if dropped > 0 {
							v.logger.Debug("dropped stale messages queued for slots before the starting duty",
								fields.RunnerRole(msgID.GetRoleType()), fields.Slot(floor), fields.Count(dropped))
						}
					}
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

// slotBelow matches messages stranded below floor — the slot of the duty the runner is currently serving.
// The consumer keeps floor in sync with the runner, so a message below it targets a slot the runner has
// already moved past and has no live duty left to serve it.
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

// popFilter selects what the consumer may pop for the runner in its current state. A runner that serves
// several slots at once (runner.MultiSlotRunner) takes anything: the consumer's single-duty notions do not
// hold for it — it reports no running duty the moment its last started slot concludes while it still serves
// that slot and stashes partials for the ones it has not started, so holding its partials would only starve
// it until the next duty-start, and were it ever to run a QBFT instance, one slot's round would say nothing
// about the others'. For every other runner: idle, only duty-starts (and any post-consensus packets a finished
// duty still awaits, see noRunningDutyFilter); an instance running but no proposal accepted for its round yet,
// that round's prepares and commits stay queued — they cannot be processed before the proposal; otherwise
// anything goes.
func popFilter(r runner.Runner, idle, multiSlot bool, rState *queue.State) queue.Filter {
	switch {
	case multiSlot:
		return queue.FilterAny
	case idle:
		return noRunningDutyFilter(r)
	case rState.HasRunningInstance && !r.HasAcceptedProposalForCurrentRound():
		return awaitingProposalFilter(specqbft.Height(rState.Slot), rState.Round)
	default:
		return queue.FilterAny
	}
}

// awaitingProposalFilter holds back the prepares and commits of the given height and round until a proposal
// is accepted for it; everything else passes.
func awaitingProposalFilter(height specqbft.Height, round specqbft.Round) queue.Filter {
	return func(m *queue.SSVMessage) bool {
		qbftMsg, ok := m.Body.(*specqbft.Message)
		if !ok || qbftMsg == nil || qbftMsg.Height != height || qbftMsg.Round != round {
			return true
		}
		return qbftMsg.MsgType != specqbft.PrepareMsgType && qbftMsg.MsgType != specqbft.CommitMsgType
	}
}

// noRunningDutyFilter selects what the consumer pops for a runner with no running duty: ExecuteDuty events
// and, for a finished duty that still expects post-consensus packets (the Gloas proposer's §6 envelope
// root), that slot's post-consensus packets. Everything else waits for the next duty to start.
func noRunningDutyFilter(r runner.Runner) queue.Filter {
	var awaitingSlot phase0.Slot
	awaiting := false
	if awaiter, ok := r.(runner.PostConsensusAwaiter); ok {
		awaitingSlot, awaiting = awaiter.AwaitingPostConsensus()
	}
	return func(m *queue.SSVMessage) bool {
		switch body := m.Body.(type) {
		case *types.EventMsg:
			return body != nil && body.Type == types.ExecuteDuty
		case *spectypes.PartialSignatureMessages:
			return awaiting && body != nil && body.Type == spectypes.PostConsensusPartialSig && body.Slot == awaitingSlot
		default:
			return false
		}
	}
}
