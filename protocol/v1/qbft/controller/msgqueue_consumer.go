package controller

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/msgqueue"
)

// MessageHandler process the msg. return error if exist
type MessageHandler func(msg *message.SSVMessage) error

func (c *Controller) startQueueConsumer(handler MessageHandler) {
	ctx, cancel := context.WithCancel(c.ctx)
	defer cancel()

	for ctx.Err() == nil {
		err := c.ConsumeQueue(handler, time.Millisecond*10)
		if err != nil {
			c.logger.Warn("could not consume queue", zap.Error(err))
		}
	}
}

// ConsumeQueue consumes messages from the msgqueue.Queue of the controller
// it checks for current state
func (c *Controller) ConsumeQueue(handler MessageHandler, interval time.Duration) error {
	ctx, cancel := context.WithCancel(c.ctx)
	defer cancel()

	for ctx.Err() == nil {
		time.Sleep(interval)

		if c.q.Len() == 0 {
			continue // no msg's at all. need to prevent cpu usage in query
		}

		lastHeight := c.signatureState.height

		if processed := c.processNoRunningInstance(handler, lastHeight); processed {
			c.logger.Debug("process none running instance is done")
			continue
		}
		if processed := c.processByState(handler); processed {
			c.logger.Debug("process by state is done")
			continue
		}
		if processed := c.processDefault(handler, lastHeight); processed {
			c.logger.Debug("process default is done")
			continue
		}
	}
	c.logger.Warn("queue consumer is closed")
	return nil
}

// processNoRunningInstance pop msg's only if no current instance running
func (c *Controller) processNoRunningInstance(handler MessageHandler, lastHeight message.Height) bool {
	if c.currentInstance != nil {
		return false // only pop when no instance running
	}

	var indexes []string
	indexes = append(indexes, msgqueue.SignedPostConsensusMsgIndex(c.Identifier, lastHeight)) // looking for last height sig msg's or late commit
	indexes = append(indexes, msgqueue.DecidedMsgIndex(c.Identifier))                         // look for decided with any height
	//indexes = append(indexes, msgqueue.SignedMsgIndex(message.SSVDecidedMsgType, c.Identifier, lastHeight, message.CommitMsgType)...)                               // looking for last height decided msgs or late decided
	indexes = append(indexes, msgqueue.SignedMsgIndex(message.SSVConsensusMsgType, c.Identifier, lastHeight, message.CommitMsgType, message.RoundChangeMsgType)...) // looking for late commit msg's or might be change round between duties
	msgs := c.popByPriority(indexes...)

	if len(msgs) == 0 || msgs[0] == nil {
		return false // no msg found
	}
	c.logger.Debug("found message in queue when no running instance", zap.String("sig state", c.signatureState.getState().toString()), zap.Int32("height", int32(c.signatureState.height)))
	err := handler(msgs[0])
	if err != nil {
		c.logger.Warn("could not handle msg", zap.Error(err))
	}
	return true // msg processed
}

// processByState if an instance is running -> get the state and get the relevant messages
func (c *Controller) processByState(handler MessageHandler) bool {
	if c.currentInstance == nil {
		return false
	}

	var msg *message.SSVMessage
	currentState := c.currentInstance.State()
	msg = c.getNextMsgForState(currentState)
	if msg == nil {
		return false // no msg found
	}
	c.logger.Debug("queue found message for state",
		zap.Int32("stage", currentState.Stage.Load()),
		zap.Int32("seq", int32(currentState.GetHeight())),
		zap.Int32("round", int32(currentState.GetRound())),
	)

	err := handler(msg)
	if err != nil {
		c.logger.Warn("could not handle msg", zap.Error(err))
	}
	return true // msg processed
}

// processDefault this phase is to allow late commit and decided msg's
// we allow late commit and decided up to 1 height back. (only to support pre fork. after fork no need to support previews height)
func (c *Controller) processDefault(handler MessageHandler, lastHeight message.Height) bool {
	var indexes []string
	indexes = append(indexes, msgqueue.SignedMsgIndex(message.SSVConsensusMsgType, c.Identifier, lastHeight-1, message.CommitMsgType)...)
	indexes = append(indexes, msgqueue.SignedMsgIndex(message.SSVDecidedMsgType, c.Identifier, lastHeight-1, message.CommitMsgType)...)
	msgs := c.popByPriority(indexes...)
	if len(msgs) > 0 {
		err := handler(msgs[0])
		if err != nil {
			c.logger.Warn("could not handle msg", zap.Error(err))
		}
		return true
	}
	return false
}

// getNextMsgForState return msgs depended on the current instance stage
func (c *Controller) getNextMsgForState(state *qbft.State) *message.SSVMessage {
	height := state.GetHeight()
	var indexes []string
	switch qbft.RoundState(state.Stage.Load()) {
	case qbft.RoundStateNotStarted:
		indexes = append(indexes, msgqueue.SignedMsgIndex(message.SSVConsensusMsgType, c.Identifier, height, message.ProposalMsgType)...)
	case qbft.RoundStatePrePrepare:
		indexes = append(indexes, msgqueue.SignedMsgIndex(message.SSVConsensusMsgType, c.Identifier, height, message.PrepareMsgType)...) // looking for propose in case is leader
	case qbft.RoundStatePrepare:
		indexes = append(indexes, msgqueue.SignedMsgIndex(message.SSVConsensusMsgType, c.Identifier, height, message.CommitMsgType)...)
	case qbft.RoundStateCommit:
		return nil // qbft.RoundStateCommit stage is NEVER set
	case qbft.RoundStateChangeRound:
		indexes = append(indexes, msgqueue.SignedMsgIndex(message.SSVConsensusMsgType, c.Identifier, height, message.RoundChangeMsgType)...)
		//case qbft.RoundStateDecided: needs to pop decided msgs in all cases not only by state
	}

	//indexes = append(indexes, msgqueue.SignedMsgIndex(message.SSVDecidedMsgType, c.Identifier, height, message.CommitMsgType)...)        // always need to look for decided msg's
	indexes = append(indexes, msgqueue.DecidedMsgIndex(c.Identifier))                                                                    // look for decided with any height
	indexes = append(indexes, msgqueue.SignedMsgIndex(message.SSVConsensusMsgType, c.Identifier, height, message.RoundChangeMsgType)...) // when sync change round need to handle msg's
	msgs := c.popByPriority(indexes...)
	if len(msgs) > 0 {
		return msgs[0]
	}

	return nil
}

// popByPriority return msgs by the order of the indexes provided
func (c *Controller) popByPriority(indexes ...string) []*message.SSVMessage {
	var msgs []*message.SSVMessage
	for _, index := range indexes {
		if len(msgs) == 0 || msgs[0] == nil {
			msgs = c.q.Pop(1, index)
		} else {
			return msgs
		}
	}
	return msgs
}
