package validator

import (
	"context"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/v2/message"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"go.uber.org/zap"
)

// HandleMessage handles a spectypes.SSVMessage.
// TODO: accept DecodedSSVMessage once p2p is upgraded to decode messages during validation.
// TODO: get rid of logger, add context
func (v *Committee) HandleMessage(logger *zap.Logger, msg *queue.DecodedSSVMessage) {
	// logger.Debug("📬 handling SSV message",
	// 	zap.Uint64("type", uint64(msg.MsgType)),
	// 	fields.Role(msg.MsgID.GetRoleType()))

	slot, err := msg.Slot()
	if err != nil {
		logger.Error("❌ could not get slot from message", fields.MessageID(msg.MsgID), zap.Error(err))
		return
	}

	v.mtx.RLock() // read v.Queues
	q, ok := v.Queues[slot]
	v.mtx.RUnlock()
	if !ok {
		q = queueContainer{
			Q: queue.WithMetrics(queue.New(1000), nil), // TODO alan: get queue opts from options
			queueState: &queue.State{
				HasRunningInstance: false,
				Height:             specqbft.Height(slot),
				Slot:               0,
				//Quorum:             options.SSVShare.Share,// TODO
			},
		}
		v.mtx.Lock()
		v.Queues[slot] = q
		v.mtx.Unlock()
		logger.Debug("missing queue for slot created", fields.Slot(slot))
	}

	if pushed := q.Q.TryPush(msg); !pushed {
		msgID := msg.MsgID.String()
		logger.Warn("❗ dropping message because the queue is full",
			zap.String("msg_type", message.MsgTypeToString(msg.MsgType)),
			zap.String("msg_id", msgID))
	} else {
		// logger.Debug("📬 queue: pushed message", fields.MessageID(msg.MsgID), fields.MessageType(msg.MsgType))
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
func (v *Committee) ConsumeQueue(
	ctx context.Context,
	logger *zap.Logger,
	slot phase0.Slot,
	handler MessageHandler,
) error {
	var q queueContainer
	err := func() error {
		v.mtx.RLock() // read v.Queues
		defer v.mtx.RUnlock()
		var ok bool
		q, ok = v.Queues[slot]
		if !ok {
			return errors.New(fmt.Sprintf("queue not found for slot %d", slot))
		}
		return nil
	}()

	// defer cancel
	defer func() {
		v.mtx.Lock()
		defer v.mtx.Unlock()
		var ok bool
		q, ok = v.Queues[slot]
		if !ok {
			logger.Warn("committee queue not found for slot while stopping the consumer", fields.Slot(slot))
			return
		}
		if q.stopQueueF == nil {
			logger.Warn("committee queue consumer stopQueueF is nil", fields.Slot(slot))
			return
		}
		q.stopQueueF()
	}()
	if err != nil {
		return err
	}

	logger.Debug("📬 queue consumer is running")

	lens := make([]int, 0, 10)

	for {
		select {
		case <-ctx.Done():
			logger.Debug("📪 queue consumer is stopped")
			return nil
		default:
			// Construct a representation of the current state.
			state := *q.queueState

			//runner := v.Runners.DutyRunnerForMsgID(msgID)
			//if runner == nil {
			//	return fmt.Errorf("could not get duty runner for msg ID %v", msgID)
			//}
			//var runningInstance *instance.Instance
			//if runner.HasRunningDuty() {
			//	runningInstance = runner.GetBaseRunner().State.RunningInstance
			//	if runningInstance != nil {
			//		decided, _ := runningInstance.IsDecided()
			//		state.HasRunningInstance = !decided
			//	}
			//}
			//state.Height = v.GetLastHeight(msgID)
			//state.Round = v.GetLastRound(msgID)
			//state.Quorum = v.Share.Quorum
			//
			//filter := queue.FilterAny
			//if !runner.HasRunningDuty() {
			//	// If no duty is running, pop only ExecuteDuty messages.
			//	filter = func(m *queue.DecodedSSVMessage) bool {
			//		e, ok := m.Body.(*types.EventMsg)
			//		if !ok {
			//			return false
			//		}
			//		return e.Type == types.ExecuteDuty
			//	}
			//} else if runningInstance != nil && runningInstance.State.ProposalAcceptedForCurrentRound == nil {
			//	// If no proposal was accepted for the current round, skip prepare & commit messages
			//	// for the current height and round.
			// filter := func(m *queue.DecodedSSVMessage) bool {
			// 	sm, ok := m.Body.(*specqbft.Message)
			// 	if !ok {
			// 		return true
			// 	}

			// 	if sm.Height != state.Height || sm.Round != state.Round {
			// 		return true
			// 	}
			// 	return sm.MsgType != specqbft.PrepareMsgType && sm.MsgType != specqbft.CommitMsgType
			// }

			// Pop the highest priority message for the current state.
			// TODO: (Alan) bring back filter
			msg := q.Q.Pop(ctx, queue.NewMessagePrioritizer(&state), queue.FilterAny)
			if ctx.Err() != nil {
				logger.Error("❗ got ctx err:", zap.Error(ctx.Err()))
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

			// Handle the message.
			if err := handler(logger, msg); err != nil {
				v.logMsg(logger, msg, "❗ could not handle message",
					fields.MessageType(msg.SSVMessage.MsgType),
					zap.Error(err))
			}
		}
	}
}

func (v *Committee) logMsg(logger *zap.Logger, msg *queue.DecodedSSVMessage, logMsg string, withFields ...zap.Field) {
	baseFields := []zap.Field{}
	switch msg.SSVMessage.MsgType {
	case spectypes.SSVConsensusMsgType:
		sm := msg.Body.(*specqbft.Message)
		baseFields = []zap.Field{
			zap.Int64("msg_height", int64(sm.Height)),
			zap.Int64("msg_round", int64(sm.Round)),
			zap.Int64("consensus_msg_type", int64(sm.MsgType)),
			zap.Any("signers", msg.SignedSSVMessage.OperatorIDs),
		}
	case spectypes.SSVPartialSignatureMsgType:
		psm := msg.Body.(*spectypes.PartialSignatureMessages)
		baseFields = []zap.Field{
			zap.Int64("signer", int64(psm.Messages[0].Signer)),
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
