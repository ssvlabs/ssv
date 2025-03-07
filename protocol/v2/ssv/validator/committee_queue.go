package validator

import (
	"context"
	"errors"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/protocol/v2/message"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
)

// HandleMessage handles a spectypes.SSVMessage.
// TODO: accept DecodedSSVMessage once p2p is upgraded to decode messages during validation.
// TODO: get rid of logger, add context
func (c *Committee) HandleMessage(ctx context.Context, logger *zap.Logger, msg *queue.SSVMessage) {
	ctx, span := tracer.Start(ctx,
		fmt.Sprintf("%s.handle_committee_message", observabilityNamespace),
		trace.WithAttributes(
			observability.ValidatorMsgIDAttribute(msg.GetID()),
			observability.ValidatorMsgTypeAttribute(msg.GetType()),
			observability.RunnerRoleAttribute(msg.GetID().GetRoleType()),
		))
	defer span.End()

	msg.Context = ctx
	slot, err := msg.Slot()
	if err != nil {
		logger.Error("❌ could not get slot from message", fields.MessageID(msg.MsgID), zap.Error(err))
		span.SetStatus(codes.Error, err.Error())
		return
	}

	span.SetAttributes(observability.BeaconSlotAttribute(slot))
	c.mtx.RLock() // read v.Queues
	q, ok := c.Queues[slot]
	c.mtx.RUnlock()
	if !ok {
		q = queueContainer{
			Q: queue.New(1000), // TODO alan: get queue opts from options
			queueState: &queue.State{
				HasRunningInstance: false,
				Height:             specqbft.Height(slot),
				Slot:               slot,
				//Quorum:             options.SSVShare.Share,// TODO
			},
		}
		c.mtx.Lock()
		c.Queues[slot] = q
		c.mtx.Unlock()
		const eventMsg = "missing queue for slot created"
		logger.Debug(eventMsg, fields.Slot(slot))
		span.AddEvent(eventMsg)
	}

	span.AddEvent("pushing message to queue")

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

//// StartQueueConsumer start ConsumeQueue with handler
//func (v *Committee) StartQueueConsumer(logger *zap.Logger, msgID spectypes.MessageID, handler MessageHandler) {
//	ctx, cancel := context.WithCancel(v.ctx)
//	defer cancel()
//
//	for ctx.Err() == nil {
//		err := v.ConsumeQueue(logger, msgID, handler)
//		if err != nil {
//			logger.Debug("❗ failed consuming queue", zap.Error(err))
//		}
//	}
//}

// ConsumeQueue consumes messages from the queue.Queue of the controller
// it checks for current state
func (c *Committee) ConsumeQueue(
	ctx context.Context,
	q queueContainer,
	logger *zap.Logger,
	slot phase0.Slot,
	handler MessageHandler,
	rnr *runner.CommitteeRunner,
) error {
	state := *q.queueState

	logger.Debug("📬 queue consumer is running")
	lens := make([]int, 0, 10)

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
				return ssvMessage.SSVMessage.MsgType != spectypes.SSVPartialSignatureMsgType
			}
		}

		// Pop the highest priority message for the current state.
		// TODO: (Alan) bring back filter
		msg := q.Q.Pop(ctx, queue.NewCommitteeQueuePrioritizer(&state), filter)
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
		if err := handler(ctx, logger, msg); err != nil {
			c.logMsg(logger, msg, "❗ could not handle message",
				fields.MessageType(msg.SSVMessage.MsgType),
				zap.Error(err))
			if errors.Is(err, runner.ErrNoValidDuties) {
				// Stop the queue consumer if the runner no longer has any valid duties.
				break
			}
		}
	}

	logger.Debug("📪 queue consumer is closed")
	return nil
}

func (c *Committee) logMsg(logger *zap.Logger, msg *queue.SSVMessage, logMsg string, withFields ...zap.Field) {
	baseFields := []zap.Field{}
	switch msg.SSVMessage.MsgType {
	case spectypes.SSVConsensusMsgType:
		sm := msg.Body.(*specqbft.Message)
		baseFields = []zap.Field{
			zap.Uint64("msg_height", uint64(sm.Height)),
			zap.Uint64("msg_round", uint64(sm.Round)),
			zap.Uint64("consensus_msg_type", uint64(sm.MsgType)),
			zap.Any("signers", msg.SignedSSVMessage.OperatorIDs),
		}
	case spectypes.SSVPartialSignatureMsgType:
		psm := msg.Body.(*spectypes.PartialSignatureMessages)
		baseFields = []zap.Field{
			zap.Uint64("signer", psm.Messages[0].Signer),
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
