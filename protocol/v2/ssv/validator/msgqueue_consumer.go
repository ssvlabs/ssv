package validator

import (
	"context"
	"github.com/bloxapp/ssv-spec/ssv"
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v2/message"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
)

// MessageHandler process the msg. return error if exist
type MessageHandler func(msg *spectypes.SSVMessage) error

// HandleMessage handles a spectypes.SSVMessage.
// TODO: accept DecodedSSVMessage once p2p is upgraded to decode messages during validation.
func (v *Validator) HandleMessage(msg *spectypes.SSVMessage) {
	//v.logger.Debug("got message, pushing to queue",
	//	zap.Int("queue_len", v.Q.Len()),
	//	zap.String("msgType", message.MsgTypeToString(msg.MsgType)),
	//	zap.String("msgID", msg.MsgID.String()),
	//)
	decodedMsg, err := queue.DecodeSSVMessage(msg)
	if err != nil {
		v.logger.Warn("failed to decode message",
			zap.Error(err),
			zap.String("msgType", message.MsgTypeToString(msg.MsgType)),
			zap.String("msgID", msg.MsgID.String()),
		)
		return
	}
	v.Q.Push(decodedMsg)
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
	logger := v.logger.With(zap.String("identifier", msgID.String()))
	logger.Debug("queue consumer is running")

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-v.ctx.Done():
			logger.Debug("queue consumer is closed")
			return nil
		case <-ticker.C:
			if v.Q.Len() == 0 {
				// Queue is empty.
				continue
			}

			// Construct a representation of the current state.
			state := *v.queueState
			runner := v.DutyRunners.DutyRunnerForMsgID(msgID)
			state.HasRunningInstance = runner != nil && runner.HasRunningDuty() &&
				runner.GetBaseRunner().State.RunningInstance != nil
			state.Height = v.GetLastHeight(msgID)

			// Sort the queue according to the current state.
			v.Q.Sort(queue.NewMessagePrioritizer(&state))

			// Pop the highest priority message and handle it.
			msg := v.Q.Pop(queue.FilterRole(msgID.GetRoleType()))
			if msg == nil {
				logger.Debug("could not pop message from queue")
				continue
			}
			err := handler(msg.SSVMessage)
			if err != nil {
				if msg.SSVMessage.MsgType != spectypes.SSVConsensusMsgType {
					psm := msg.Body.(*ssv.SignedPartialSignatureMessage)
					logger.Warn("could not handle message (partial signature)", zap.String("error", err.Error()),
						zap.Int64("signer", int64(psm.Signer)))
				} else {
					sm := msg.Body.(*specqbft.SignedMessage)
					logger.Warn("could not handle message (consensus)", zap.String("error", err.Error()),
						zap.Int64("msg_height", int64(sm.Message.Height)),
						zap.Int64("msg_round", int64(sm.Message.Round)),
						zap.Int64("consensus_msg_type", int64(sm.Message.MsgType)),
						zap.Any("signers", sm.Signers))
				}
				continue
			}
		}
	}
}

// GetLastHeight returns the last height for the given identifier
func (v *Validator) GetLastHeight(identifier spectypes.MessageID) specqbft.Height {
	r := v.DutyRunners.DutyRunnerForMsgID(identifier)
	if r == nil {
		return specqbft.Height(0)
	}
	return r.GetBaseRunner().QBFTController.Height
}
