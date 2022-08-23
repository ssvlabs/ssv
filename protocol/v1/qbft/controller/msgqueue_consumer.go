package controller

import (
	"context"
	"encoding/hex"
	"sync/atomic"
	"time"

	spectypes "github.com/bloxapp/ssv-spec/types"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/msgqueue"
)

// MessageHandler process the msg. return error if exist
type MessageHandler func(msg *spectypes.SSVMessage) error

// StartQueueConsumer start ConsumeQueue with handler
func (c *Controller) StartQueueConsumer(handler MessageHandler) {
	ctx, cancel := context.WithCancel(c.Ctx)
	defer cancel()

	for ctx.Err() == nil {
		err := c.ConsumeQueue(handler, time.Millisecond*50)
		if err != nil {
			c.Logger.Warn("could not consume queue", zap.Error(err))
		}
	}
}

// ConsumeQueue consumes messages from the msgqueue.Queue of the controller
// it checks for current state
func (c *Controller) ConsumeQueue(handler MessageHandler, interval time.Duration) error {
	ctx, cancel := context.WithCancel(c.Ctx)
	defer cancel()

	identifier := hex.EncodeToString(c.Identifier)

	for ctx.Err() == nil {
		time.Sleep(interval)

		// no msg's in the queue
		if c.Q.Len() == 0 {
			time.Sleep(interval)
			continue // no msg's at all. need to prevent cpu usage in query
		}
		// avoid process messages on fork
		if atomic.LoadUint32(&c.State) == Forking {
			time.Sleep(interval)
			continue
		}

		lastSlot := spec.Slot(0) // no slot 0.
		if c.SignatureState.duty != nil {
			lastSlot = c.SignatureState.duty.Slot
		}
		lastHeight := c.SignatureState.getHeight()

		if processed := c.processNoRunningInstance(handler, identifier, lastHeight, lastSlot); processed {
			c.Logger.Debug("process none running instance is done")
			continue
		}
		if processed := c.processByState(handler, identifier); processed {
			c.Logger.Debug("process by state is done")
			continue
		}
		if processed := c.processDefault(handler, identifier, lastHeight); processed {
			c.Logger.Debug("process default is done")
			continue
		}

		// clean all old messages. (when stuck on change round stage, msgs not deleted)
		c.Q.Clean(func(index msgqueue.Index) bool {
			oldHeight := index.H >= 0 && index.H <= (lastHeight-2) // remove all msg's that are 2 heights old. not post consensus & decided
			oldSlot := index.S > 0 && index.S < lastSlot
			return oldHeight || oldSlot
		})
	}
	c.Logger.Warn("queue consumer is closed")
	return nil
}

// processNoRunningInstance pop msg's only if no current instance running
func (c *Controller) processNoRunningInstance(
	handler MessageHandler,
	identifier string,
	lastHeight specqbft.Height,
	lastSlot spec.Slot,
) bool {
	instance := c.GetCurrentInstance()
	if instance != nil {
		return false // only pop when no instance running
	}

	logger := c.Logger.With(
		zap.String("sig state", c.SignatureState.getState().toString()),
		zap.Int32("height", int32(lastHeight)),
		zap.Int32("slot", int32(lastSlot)))

	iterator := msgqueue.NewIndexIterator().Add(func() msgqueue.Index {
		return msgqueue.SignedPostConsensusMsgIndex(identifier, lastSlot)
	}, func() msgqueue.Index {
		return msgqueue.DecidedMsgIndex(identifier)
	}, func() msgqueue.Index {
		indices := msgqueue.SignedMsgIndex(spectypes.SSVConsensusMsgType, identifier, lastHeight, specqbft.CommitMsgType)
		if len(indices) == 0 {
			return msgqueue.Index{}
		}
		return indices[0]
	})

	msgs := c.Q.PopIndices(1, iterator)

	if len(msgs) == 0 || msgs[0] == nil {
		return false // no msg found
	}
	err := handler(msgs[0])
	if err != nil {
		logger.Warn("could not handle msg", zap.Error(err))
	}
	return true // msg processed
}

// processByState if an instance is running -> get the state and get the relevant messages
func (c *Controller) processByState(handler MessageHandler, identifier string) bool {
	currentInstance := c.GetCurrentInstance()
	if c.GetCurrentInstance() == nil {
		return false
	}

	var msg *spectypes.SSVMessage

	currentState := currentInstance.State()
	msg = c.getNextMsgForState(currentState, identifier)
	if msg == nil {
		return false // no msg found
	}
	c.Logger.Debug("queue found message for state",
		zap.Int32("stage", currentState.Stage.Load()),
		zap.Int32("seq", int32(currentState.GetHeight())),
		zap.Int32("round", int32(currentState.GetRound())),
	)

	err := handler(msg)
	if err != nil {
		c.Logger.Warn("could not handle msg", zap.Error(err))
	}
	return true // msg processed
}

// processDefault this phase is to allow late commit and decided msg's
// we allow late commit and decided up to 1 height back. (only to support pre fork. after fork no need to support previews height)
func (c *Controller) processDefault(handler MessageHandler, identifier string, lastHeight specqbft.Height) bool {
	iterator := msgqueue.NewIndexIterator().
		Add(func() msgqueue.Index {
			indices := msgqueue.SignedMsgIndex(spectypes.SSVConsensusMsgType, identifier, lastHeight-1, specqbft.CommitMsgType)
			if len(indices) == 0 {
				return msgqueue.Index{}
			}
			return indices[0]
		}).Add(func() msgqueue.Index {
		indices := msgqueue.SignedMsgIndex(spectypes.SSVDecidedMsgType, identifier, lastHeight-1, specqbft.CommitMsgType)
		if len(indices) == 0 {
			return msgqueue.Index{}
		}
		return indices[0]
	})
	msgs := c.Q.PopIndices(1, iterator)

	if len(msgs) > 0 {
		err := handler(msgs[0])
		if err != nil {
			c.Logger.Warn("could not handle msg", zap.Error(err))
		}
		return true
	}
	return false
}

// getNextMsgForState return msgs depended on the current instance stage
func (c *Controller) getNextMsgForState(state *qbft.State, identifier string) *spectypes.SSVMessage {
	height := state.GetHeight()
	stage := qbft.RoundState(state.Stage.Load())

	iterator := msgqueue.NewIndexIterator().
		Add(func() msgqueue.Index {
			return stateIndex(identifier, stage, height)
		}).
		Add(func() msgqueue.Index {
			return msgqueue.DecidedMsgIndex(identifier)
		}).
		Add(func() msgqueue.Index {
			indices := msgqueue.SignedMsgIndex(spectypes.SSVConsensusMsgType, identifier, height, specqbft.RoundChangeMsgType)
			if len(indices) == 0 {
				return msgqueue.Index{}
			}
			return indices[0]
		})
	msgs := c.Q.PopIndices(1, iterator)
	if len(msgs) == 0 {
		return nil
	}
	return msgs[0]
}

// processOnFork this phase is to allow process remaining decided messages that arrived late to the msg queue
func (c *Controller) processAllDecided(handler MessageHandler) {
	idx := msgqueue.DecidedMsgIndex(hex.EncodeToString(c.Identifier))
	msgs := c.Q.Pop(1, idx)
	for len(msgs) > 0 {
		err := handler(msgs[0])
		if err != nil {
			c.Logger.Warn("could not handle msg", zap.Error(err))
		}
		msgs = c.Q.Pop(1, idx)
	}
}

func stateIndex(identifier string, stage qbft.RoundState, height specqbft.Height) msgqueue.Index {
	var res []msgqueue.Index
	switch stage {
	case qbft.RoundStateNotStarted:
		res = msgqueue.SignedMsgIndex(spectypes.SSVConsensusMsgType, identifier, height, specqbft.ProposalMsgType)
	case qbft.RoundStateProposal:
		res = msgqueue.SignedMsgIndex(spectypes.SSVConsensusMsgType, identifier, height, specqbft.PrepareMsgType) // looking for propose in case is leader
	case qbft.RoundStatePrepare:
		res = msgqueue.SignedMsgIndex(spectypes.SSVConsensusMsgType, identifier, height, specqbft.CommitMsgType)
	case qbft.RoundStateCommit:
		return msgqueue.Index{} // qbft.RoundStateCommit stage is NEVER set
	case qbft.RoundStateChangeRound:
		res = msgqueue.SignedMsgIndex(spectypes.SSVConsensusMsgType, identifier, height, specqbft.RoundChangeMsgType)
		//case qbft.RoundStateDecided: needs to pop decided msgs in all cases not only by state
	}
	if len(res) == 0 {
		return msgqueue.Index{}
	}
	return res[0]
}
