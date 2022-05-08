package controller

import (
	"context"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/msgqueue"
	"go.uber.org/zap"
	"time"
)

func (c *Controller) startQueueConsumer() {
	ctx, cancel := context.WithCancel(c.ctx)
	defer cancel()

	for ctx.Err() == nil {
		err := c.ConsumeQueue(time.Millisecond * 10)
		if err != nil {
			c.logger.Warn("could not consume queue", zap.Error(err))
		}
	}
}

// ConsumeQueue consumes messages from the msgqueue.Queue of the controller
// it checks for current state
func (c *Controller) ConsumeQueue(interval time.Duration) error {
	ctx, cancel := context.WithCancel(c.ctx)
	defer cancel()

	for ctx.Err() == nil {
		time.Sleep(interval)
		if c.currentInstance == nil {
			// TODO: complete, currently just trying to peek any message
			// 		it might be better to get last state (c.ibftStorage.GetCurrentInstance())
			//		and try to get messages from that height
			var msgs []*message.SSVMessage
			sigTimerRunning := c.signatureState.getState() == StateRunning
			if sigTimerRunning {
				msgs = c.q.Pop(1, msgqueue.SignedPostConsensusMsgIndex(c.Identifier, c.signatureState.height)) // sigs
			}

			if len(msgs) == 0 || msgs[0] == nil {
				msgs = c.q.Pop(1, msgqueue.SignedMsgIndex(message.SSVDecidedMsgType, c.Identifier, c.signatureState.height, message.CommitMsgType, message.RoundChangeMsgType)...)
			}
			if len(msgs) == 0 || msgs[0] == nil {
				continue
			}
			c.logger.Debug("process message when instance is nil")
			err := c.messageHandler(msgs[0])
			if err != nil {
				c.logger.Warn("could not handle msg", zap.Error(err))
			}
			continue
		}
		// if an instance is running -> get the state and get the relevant messages
		var msg *message.SSVMessage
		currentState := c.currentInstance.State()
		height := currentState.GetHeight()
		sigMsgs := c.q.Pop(1, msgqueue.SignedPostConsensusMsgIndex(c.Identifier, height))
		if len(sigMsgs) > 0 {
			// got post consensus message for the current sequence
			c.logger.Debug("pop post consensus msg")
			msg = sigMsgs[0]
		} else {
			msg = c.getNextMsgForState(currentState)
			if msg == nil {
				continue
			}
			c.logger.Debug("queue found message for state", zap.Int32("stage", currentState.Stage.Load()), zap.Int32("seq", int32(currentState.GetHeight())), zap.Int32("round", int32(currentState.GetRound())))
		}

		err := c.messageHandler(msg)
		if err != nil {
			c.logger.Warn("could not handle msg", zap.Error(err))
		}
		c.logger.Debug("message handler is done")
	}
	c.logger.Warn("queue consumer is closed")
	return nil
}

func (c *Controller) getNextMsgForState(state *qbft.State) *message.SSVMessage {
	height := state.GetHeight()
	var msgs []*message.SSVMessage
	switch qbft.RoundState(state.Stage.Load()) {
	case qbft.RoundState_NotStarted:
		msgs = c.q.Pop(1, msgqueue.DefaultMsgIndex(message.SSVConsensusMsgType, c.Identifier))
	case qbft.RoundState_PrePrepare:
		msgs = c.q.Pop(1, msgqueue.SignedMsgIndex(message.SSVConsensusMsgType, c.Identifier, height, message.PrepareMsgType, message.RoundChangeMsgType)...) // looking for propose in case is leader
	case qbft.RoundState_Prepare:
		msgs = c.q.Pop(1, msgqueue.SignedMsgIndex(message.SSVConsensusMsgType, c.Identifier, height, message.CommitMsgType, message.RoundChangeMsgType)...)
	case qbft.RoundState_Commit:
		return nil // qbft.RoundState_Commit stage is NEVER set
	case qbft.RoundState_ChangeRound:
		msgs = c.q.Pop(1, msgqueue.SignedMsgIndex(message.SSVConsensusMsgType, c.Identifier, height, message.RoundChangeMsgType)...)
		//case qbft.RoundState_Decided: only after instance is nilled
		//case qbft.RoundState_Stopped: only after instance is nilled
	}
	if len(msgs) > 0 {
		return msgs[0]
	}
	return nil
}
