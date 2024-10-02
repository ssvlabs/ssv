package validator

import (
	"context"
	"fmt"

	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/genesis/message"
	"github.com/ssvlabs/ssv/protocol/genesis/qbft/instance"
	"github.com/ssvlabs/ssv/protocol/genesis/ssv/genesisqueue"
	"github.com/ssvlabs/ssv/protocol/genesis/types"
)

// MessageHandler process the msg. return error if exist
type MessageHandler func(logger *zap.Logger, msg *genesisqueue.GenesisSSVMessage) error

// queueContainer wraps a queue with its corresponding state
type queueContainer struct {
	Q          genesisqueue.Queue
	queueState *genesisqueue.State
}

// HandleMessage handles a genesisspectypes.SSVMessage.
// TODO: accept DecodedSSVMessage once p2p is upgraded to decode messages during validation.
// TODO: get rid of logger, add context
func (v *Validator) HandleMessage(logger *zap.Logger, msg *genesisqueue.GenesisSSVMessage) {
	v.mtx.RLock() // read v.Queues
	defer v.mtx.RUnlock()

	// logger.Debug("📬 handling SSV message",
	// 	zap.Uint64("type", uint64(msg.MsgType)),
	// 	fields.Role(msg.MsgID.GetRoleType()))

	if q, ok := v.Queues[msg.MsgID.GetRoleType()]; ok {
		if pushed := q.Q.TryPush(msg); !pushed {
			msgID := msg.MsgID.String()
			logger.Warn("❗ dropping message because the queue is full",
				zap.String("msg_type", message.MsgTypeToString(msg.MsgType)),
				zap.String("msg_id", msgID))
		}
		//logger.Debug("📬 queue: pushed message", fields.MessageID(spectypes.MessageID(msg.MsgID)), fields.MessageType(spectypes.MsgType(msg.MsgType)))
	} else {
		logger.Error("❌ missing queue for role type", fields.BeaconRole(spectypes.BeaconRole(msg.MsgID.GetRoleType())))
	}
}

// StartQueueConsumer start ConsumeQueue with handler
func (v *Validator) StartQueueConsumer(logger *zap.Logger, msgID genesisspectypes.MessageID, handler MessageHandler) {
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
func (v *Validator) ConsumeQueue(logger *zap.Logger, msgID genesisspectypes.MessageID, handler MessageHandler) error {
	ctx, cancel := context.WithCancel(v.ctx)
	defer cancel()

	var q queueContainer
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

	lens := make([]int, 0, 10)

	for ctx.Err() == nil {
		// Construct a representation of the current state.
		state := *q.queueState
		runner := v.DutyRunners.DutyRunnerForMsgID(msgID)
		if runner == nil {
			return fmt.Errorf("could not get duty runner for msg ID %v", msgID)
		}
		var runningInstance *instance.Instance
		if runner.HasRunningDuty() {
			runningInstance = runner.GetBaseRunner().State.RunningInstance
			if runningInstance != nil {
				decided, _ := runningInstance.IsDecided()
				state.HasRunningInstance = !decided
			}
		}
		state.Height = v.GetLastHeight(msgID)
		state.Round = v.GetLastRound(msgID)
		state.Quorum = v.Share.Quorum

		filter := genesisqueue.FilterAny
		if !runner.HasRunningDuty() {
			// If no duty is running, pop only ExecuteDuty messages.
			filter = func(m *genesisqueue.GenesisSSVMessage) bool {
				e, ok := m.Body.(*types.EventMsg)
				if !ok {
					return false
				}
				return e.Type == types.ExecuteDuty
			}
		} else if runningInstance != nil && runningInstance.State.ProposalAcceptedForCurrentRound == nil {
			// If no proposal was accepted for the current round, skip prepare & commit messages
			// for the current height and round.
			filter = func(m *genesisqueue.GenesisSSVMessage) bool {
				sm, ok := m.Body.(*genesisspecqbft.SignedMessage)
				if !ok {
					return true
				}
				if sm.Message.Height != state.Height || sm.Message.Round != state.Round {
					return true
				}
				return sm.Message.MsgType != genesisspecqbft.PrepareMsgType && sm.Message.MsgType != genesisspecqbft.CommitMsgType
			}
		}

		// Pop the highest priority message for the current state.
		msg := q.Q.Pop(ctx, genesisqueue.NewMessagePrioritizer(&state), filter)
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
				fields.MessageID(spectypes.MessageID(msg.MsgID)), fields.MessageType(spectypes.MsgType(msg.MsgType)),
				zap.Ints("past_10_lengths", lens))
			lens = lens[:0]
		}

		// Handle the message.
		if err := handler(logger, msg); err != nil {
			v.logMsg(logger, msg, "❗ could not handle message",
				fields.MessageType(spectypes.MsgType(msg.SSVMessage.MsgType)),
				zap.Error(err))
		}
	}

	logger.Debug("📪 queue consumer is closed")
	return nil
}

func (v *Validator) logMsg(logger *zap.Logger, msg *genesisqueue.GenesisSSVMessage, logMsg string, withFields ...zap.Field) {
	baseFields := []zap.Field{}
	switch msg.SSVMessage.MsgType {
	case genesisspectypes.SSVConsensusMsgType:
		sm := msg.Body.(*genesisspecqbft.SignedMessage)
		baseFields = []zap.Field{
			zap.Uint64("msg_height", uint64(sm.Message.Height)),
			zap.Uint64("msg_round", uint64(sm.Message.Round)),
			zap.Uint64("consensus_msg_type", uint64(sm.Message.MsgType)),
			zap.Any("signers", sm.Signers),
		}
	case genesisspectypes.SSVPartialSignatureMsgType:
		psm := msg.Body.(*genesisspectypes.SignedPartialSignatureMessage)
		baseFields = []zap.Field{
			zap.Uint64("signer", psm.Signer),
			fields.Slot(psm.Message.Slot),
		}
	}
	logger.Debug(logMsg, append(baseFields, withFields...)...)
}

// GetLastHeight returns the last height for the given identifier
func (v *Validator) GetLastHeight(identifier genesisspectypes.MessageID) genesisspecqbft.Height {
	r := v.DutyRunners.DutyRunnerForMsgID(identifier)
	if r == nil {
		return genesisspecqbft.Height(0)
	}
	if ctrl := r.GetBaseRunner().QBFTController; ctrl != nil {
		return ctrl.Height
	}
	return genesisspecqbft.Height(0)
}

// GetLastRound returns the last height for the given identifier
func (v *Validator) GetLastRound(identifier genesisspectypes.MessageID) genesisspecqbft.Round {
	r := v.DutyRunners.DutyRunnerForMsgID(identifier)
	if r == nil {
		return genesisspecqbft.Round(1)
	}
	if r != nil && r.HasRunningDuty() {
		inst := r.GetBaseRunner().State.RunningInstance
		if inst != nil {
			return inst.State.Round
		}
	}
	return genesisspecqbft.Round(1)
}
