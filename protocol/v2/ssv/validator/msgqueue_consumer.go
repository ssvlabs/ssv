package validator

import (
	"context"
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
func (v *Validator) HandleMessage(msg *spectypes.SSVMessage) {
	v.logger.Debug("got message, pushing to queue",
		zap.Int("queue_len", v.Q.Len()),
		zap.String("msgType", message.MsgTypeToString(msg.MsgType)),
		zap.String("msgID", msg.MsgID.String()),
	)
	decodedMsg, err := queue.DecodeSSVMessage(msg)
	if err != nil {
		v.logger.Error("failed to decode message",
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
			v.logger.Warn("could not consume queue", zap.Error(err))
		}
	}
}

// ConsumeQueue consumes messages from the queue.Queue of the controller
// it checks for current state
func (v *Validator) ConsumeQueue(msgID spectypes.MessageID, handler MessageHandler, interval time.Duration) error {
	logger := v.logger.With(zap.String("identifier", msgID.String()))
	logger.Warn("queue consumer is running")

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-v.ctx.Done():
			return nil
		case <-ticker.C:
			if v.Q.Len() == 0 {
				// Queue is empty.
				continue
			}

			// Construct a representation of the current state.
			runner := v.DutyRunners.DutyRunnerForMsgID(msgID)
			state := &queue.State{
				HasRunningInstance: runner != nil && runner.HasRunningDuty() &&
					runner.GetBaseRunner().State.RunningInstance != nil,
				Height: v.GetLastHeight(msgID),
				Slot:   0,
				Quorum: v.Share.Quorum,
			}

			// Sort the queue according to the current state.
			v.Q.Sort(queue.NewMessagePrioritizer(state))

			// Pop the highest priority message and handle it.
			msg := v.Q.Pop(msgID.GetRoleType())
			handler(msg.SSVMessage)
		}
	}

	logger.Warn("queue consumer is closed")

	return nil
}

// GetLastHeight returns the last height for the given identifier
func (v *Validator) GetLastHeight(identifier spectypes.MessageID) specqbft.Height {
	r := v.DutyRunners.DutyRunnerForMsgID(identifier)
	if r == nil {
		return specqbft.Height(0)
	}
	// ctrl := r.GetBaseRunner().QBFTController
	// if ctrl == nil {
	//	return specqbft.Height(0)
	//}
	// return state.LastHeight
	return r.GetBaseRunner().QBFTController.Height
}
