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
			msgs := c.q.Pop(msgqueue.SignedPostConsensusMsgIndex(c.Identifier, c.signatureState.height), 1) // sigs
			if len(msgs) == 0 || msgs[0] == nil {
				msgs = c.q.Pop(msgqueue.SignedMsgIndex(message.SSVDecidedMsgType, c.Identifier, c.signatureState.height, message.CommitMsgType), 1) // decided
			}
			//if len(msgs) == 0 || msgs[0] == nil {
			//	msgs = c.q.Pop(msgqueue.DefaultMsgIndex(message.SSVConsensusMsgType, c.Identifier), 1) // other ibft msgs
			//}
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
		sigMsgs := c.q.Pop(msgqueue.SignedPostConsensusMsgIndex(c.Identifier, height), 1)
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
		msgs = c.q.Peek(msgqueue.DefaultMsgIndex(message.SSVConsensusMsgType, c.Identifier), 1) // TODO here waiting for prePrepare msg // TODO when move to prePrepare, the same msg called twice
	case qbft.RoundState_PrePrepare:
		msgs = c.q.Pop(msgqueue.SignedMsgIndex(message.SSVConsensusMsgType, c.Identifier, height, message.PrepareMsgType), 1)
	case qbft.RoundState_Prepare:
		msgs = c.q.Pop(msgqueue.SignedMsgIndex(message.SSVConsensusMsgType, c.Identifier, height, message.CommitMsgType), 1)
	case qbft.RoundState_Commit:
		return nil // qbft.RoundState_Commit stage is NEVER set
	case qbft.RoundState_ChangeRound:
		msgs = c.q.Pop(msgqueue.SignedMsgIndex(message.SSVConsensusMsgType, c.Identifier, height, message.RoundChangeMsgType), 1)
		//case qbft.RoundState_Decided: only after instance is nilled
		//case qbft.RoundState_Stopped:
	}
	if len(msgs) > 0 {
		return msgs[0]
	}
	return nil
}
