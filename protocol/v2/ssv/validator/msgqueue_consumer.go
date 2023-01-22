package validator

import (
	"context"
	"fmt"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/ssv"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/protocol/v2/message"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"

	"github.com/pkg/errors"
	"go.uber.org/zap"
	"time"
)

// MessageHandler process the msg. return error if exist
type MessageHandler func(msg *queue.DecodedSSVMessage) error

// queueContainer wraps a queue with its corresponding state
type queueContainer struct {
	Q          queue.Queue
	queueState *queue.State
}

// HandleMessage handles a spectypes.SSVMessage.
// TODO: accept DecodedSSVMessage once p2p is upgraded to decode messages during validation.
func (v *Validator) HandleMessage(msg *spectypes.SSVMessage) {
	if q, ok := v.Queues[msg.MsgID.GetRoleType()]; ok {
		decodedMsg, err := queue.DecodeSSVMessage(msg)
		if err != nil {
			v.logger.Warn("failed to decode message",
				zap.Error(err),
				zap.String("msgType", message.MsgTypeToString(msg.MsgType)),
				zap.String("msgID", msg.MsgID.String()),
			)
			return
		}
		q.Q.Push(decodedMsg)
	} else {
		v.logger.Error("missing queue for role type", zap.String("role", msg.MsgID.GetRoleType().String()))
	}
}

// StartQueueConsumer start ConsumeQueue with handler
func (v *Validator) StartQueueConsumer(msgID spectypes.MessageID, handler MessageHandler) {
	ctx, cancel := context.WithCancel(v.ctx)
	defer cancel()

	for ctx.Err() == nil {
		err := v.ConsumeQueue(msgID, handler, time.Millisecond*50)
		if err != nil {
			v.logger.Debug("failed consuming queue", zap.Error(err))
		}
	}
}

// ConsumeQueue consumes messages from the queue.Queue of the controller
// it checks for current state
func (v *Validator) ConsumeQueue(msgID spectypes.MessageID, handler MessageHandler, interval time.Duration) error {
	ctx, cancel := context.WithCancel(v.ctx)
	defer cancel()

	q, ok := v.Queues[msgID.GetRoleType()]
	if !ok {
		return errors.New(fmt.Sprintf("queue not found for role %s", msgID.GetRoleType().String()))
	}

	logger := v.logger.With(zap.String("identifier", msgID.String()))

	logger.Debug("queue consumer is running")

	for ctx.Err() == nil {

		if q.Q.IsEmpty() {
			time.Sleep(interval)
			continue
		}
		// Construct a representation of the current state.
		state := *q.queueState
		runner := v.DutyRunners.DutyRunnerForMsgID(msgID)
		if runner != nil && runner.HasRunningDuty() {
			inst := runner.GetBaseRunner().State.RunningInstance
			if inst != nil {
				decided, _ := inst.IsDecided()
				state.HasRunningInstance = !decided
			}
		}
		state.Height = v.GetLastHeight(msgID)
		state.Round = v.GetLastRound(msgID)

		// Pop the highest priority message and handle it.
		msg := q.Q.Pop(queue.NewMessagePrioritizer(&state))
		if msg == nil {
			logger.Debug("could not pop message from queue")
			time.Sleep(interval)
			continue
		}

		if err := handler(msg); err != nil {
			switch msg.SSVMessage.MsgType {
			case spectypes.SSVConsensusMsgType:
				sm := msg.Body.(*specqbft.SignedMessage)
				logger.Debug("could not handle message (consensus)", zap.Error(err),
					zap.Int64("msg_height", int64(sm.Message.Height)),
					zap.Int64("msg_round", int64(sm.Message.Round)),
					zap.Int64("consensus_msg_type", int64(sm.Message.MsgType)),
					zap.Any("signers", sm.Signers),
					zap.Any("LIOR:state", state))

			case spectypes.SSVPartialSignatureMsgType:
				psm := msg.Body.(*ssv.SignedPartialSignatureMessage)
				logger.Debug("could not handle message (partial signature)", zap.Error(err),
					zap.Int64("signer", int64(psm.Signer)))
			}
		}
	}
	logger.Debug("queue consumer is closed")
	return nil
}

// GetLastHeight returns the last height for the given identifier
func (v *Validator) GetLastHeight(identifier spectypes.MessageID) specqbft.Height {
	r := v.DutyRunners.DutyRunnerForMsgID(identifier)
	if r == nil {
		return specqbft.Height(0)
	}
	return r.GetBaseRunner().QBFTController.Height
}

// GetLastRound returns the last height for the given identifier
func (v *Validator) GetLastRound(identifier spectypes.MessageID) specqbft.Round {
	r := v.DutyRunners.DutyRunnerForMsgID(identifier)
	if r == nil {
		return specqbft.Round(1)
	}
	if r != nil && r.HasRunningDuty() {
		inst := r.GetBaseRunner().State.RunningInstance
		if inst != nil {
			return inst.State.Round
		}
	}
	return specqbft.Round(1)
}
