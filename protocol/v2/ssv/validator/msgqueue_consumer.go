package validator

import (
	"context"
	"encoding/hex"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v2/qbft/instance"
	"time"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v2/ssv/msgqueue"

	"github.com/patrickmn/go-cache"
	"go.uber.org/zap"
)

// MessageHandler process the msg. return error if exist
type MessageHandler func(msg *spectypes.SSVMessage) error

// StartQueueConsumer start ConsumeQueue with handler
func (v *Validator) StartQueueConsumer(handler MessageHandler) {
	ctx, cancel := context.WithCancel(v.ctx)
	defer cancel()

	for ctx.Err() == nil {
		err := v.ConsumeQueue(handler, time.Millisecond*50)
		if err != nil {
			v.logger.Warn("could not consume queue", zap.Error(err))
		}
	}
}

func (v *Validator) GetCurrentInstance() *instance.Instance {
	// TODO implement
	return nil
}

// ConsumeQueue consumes messages from the msgqueue.Queue of the controller
// it checks for current state
func (v *Validator) ConsumeQueue(handler MessageHandler, interval time.Duration) error {
	ctx, cancel := context.WithCancel(v.ctx)
	defer cancel()

	identifier := hex.EncodeToString(v.Identifier)
	higherCache := cache.New(time.Second*12, time.Second*24)

	for ctx.Err() == nil {
		time.Sleep(interval)

		// no msg's in the queue
		if v.Q.Len() == 0 {
			time.Sleep(interval)
			continue // no msg's at all. need to prevent cpu usage in query
		}
		//// avoid process messages on fork
		//if atomic.LoadUint32(&v.State) == Forking {
		//	time.Sleep(interval)
		//	continue
		//}
		//
		// TODO
		lastSlot := spec.Slot(0) // v.SignatureState.lastSlot // no slot - 0.
		lastHeight := specqbft.Height(0) // v.GetHeight()

		if processed := v.processNoRunningInstance(handler, identifier, lastHeight, lastSlot); processed {
			v.logger.Debug("process none running instance is done")
			continue
		}
		if processed := v.processByState(handler, identifier); processed {
			v.logger.Debug("process by state is done")
			continue
		}
		if processed := v.processHigherHeight(handler, identifier, lastHeight, higherCache); processed {
			v.logger.Debug("process higher height is done")
			continue
		}
		if processed := v.processLateCommit(handler, identifier, lastHeight); processed {
			v.logger.Debug("process default is done")
			continue
		}

		// clean all old messages. (when stuck on change round stage, msgs not deleted)
		cleaned := v.Q.Clean(func(index msgqueue.Index) bool {
			oldHeight := index.H >= 0 && index.H <= (lastHeight-2) // remove all msg's that are 2 heights old. not post consensus & decided
			oldSlot := index.S > 0 && index.S < lastSlot
			return oldHeight || oldSlot
		})
		if cleaned > 0 {
			v.logger.Debug("indexes cleaned from queue", zap.Int64("count", cleaned))
		}
	}
	v.logger.Warn("queue consumer is closed")
	return nil
}

// processNoRunningInstance pop msg's only if no current instance running
func (v *Validator) processNoRunningInstance(
	handler MessageHandler,
	identifier string,
	lastHeight specqbft.Height,
	lastSlot spec.Slot,
) bool {
	instance := v.GetCurrentInstance()
	if instance != nil {
		return false // only pop when no instance running
	}

	logger := v.logger.With(
		//zap.String("sig state", c.SignatureState.getState().toString()),
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

	msgs := v.Q.PopIndices(1, iterator)

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
func (v *Validator) processByState(handler MessageHandler, identifier string) bool {
	//currentInstance := v.GetCurrentInstance()
	//if currentInstance == nil {
	//	return false
	//}

	var msg *spectypes.SSVMessage

	//currentState := currentInstance.GetState()
	//msg = c.getNextMsgForState(currentState, identifier)
	//if msg == nil {
	//	return false // no msg found
	//}
	//v.logger.Debug("queue found message for state",
	//	zap.Int32("stage", currentState.Stage.Load()),
	//	zap.Int32("seq", int32(currentState.GetHeight())),
	//	zap.Int32("round", int32(currentState.GetRound())),
	//)

	err := handler(msg)
	if err != nil {
		v.logger.Warn("could not handle msg", zap.Error(err))
	}
	return true // msg processed
}

// processHigherHeight fetch any message with higher height than last height
func (v *Validator) processHigherHeight(handler MessageHandler, identifier string, lastHeight specqbft.Height, higherCache *cache.Cache) bool {
	msgs := v.Q.WithIterator(1, true, func(index msgqueue.Index) bool {
		key := index.String()
		if _, found := higherCache.Get(key); !found {
			higherCache.Set(key, 0, cache.DefaultExpiration)
		} else {
			return false // skip msg
		}

		return index.ID == identifier && index.H > lastHeight
	})

	if len(msgs) > 0 {
		err := handler(msgs[0])
		if err != nil {
			v.logger.Warn("could not handle msg", zap.Error(err))
		}
		return true
	}
	return false
}

// processLateCommit this phase is to allow late commit and decided msg's
// we allow late commit and decided up to 1 height back. (only to support pre fork. after fork no need to support previews height)
func (v *Validator) processLateCommit(handler MessageHandler, identifier string, lastHeight specqbft.Height) bool {
	iterator := msgqueue.NewIndexIterator().
		Add(func() msgqueue.Index {
			indices := msgqueue.SignedMsgIndex(spectypes.SSVConsensusMsgType, identifier, lastHeight-1, specqbft.CommitMsgType)
			if len(indices) == 0 {
				return msgqueue.Index{}
			}
			return indices[0]
		}).Add(func() msgqueue.Index {
		indices := msgqueue.SignedMsgIndex(message.SSVDecidedMsgType, identifier, lastHeight-1, specqbft.CommitMsgType)
		if len(indices) == 0 {
			return msgqueue.Index{}
		}
		return indices[0]
	})
	msgs := v.Q.PopIndices(1, iterator)

	if len(msgs) > 0 {
		err := handler(msgs[0])
		if err != nil {
			v.logger.Warn("could not handle msg", zap.Error(err))
		}
		return true
	}
	return false
}

// getNextMsgForState return msgs depended on the current instance stage
func (v *Validator) getNextMsgForState(state *qbft.State, identifier string) *spectypes.SSVMessage {
	height := state.GetHeight()
	stage := qbft.RoundState(state.Stage.Load())

	iterator := msgqueue.NewIndexIterator()

	stats := stateIndex(identifier, stage, height)

	for _, idx := range stats {
		iterator.AddIndex(idx)
	}

	iterator.
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

	msgs := v.Q.PopIndices(1, iterator)
	if len(msgs) == 0 {
		return nil
	}
	return msgs[0]
}

// processOnFork this phase is to allow process remaining decided messages that arrived late to the msg queue
func (v *Validator) processAllDecided(handler MessageHandler, identifier []byte) {
	idx := msgqueue.DecidedMsgIndex(hex.EncodeToString(identifier))
	msgs := v.Q.Pop(1, idx)
	for len(msgs) > 0 {
		err := handler(msgs[0])
		if err != nil {
			v.logger.Warn("could not handle msg", zap.Error(err))
		}
		msgs = v.Q.Pop(1, idx)
	}
}

func stateIndex(identifier string, stage qbft.RoundState, height specqbft.Height) []msgqueue.Index {
	var res []msgqueue.Index
	switch stage {
	case qbft.RoundStateReady:
		res = msgqueue.SignedMsgIndex(spectypes.SSVConsensusMsgType, identifier, height, specqbft.ProposalMsgType, specqbft.PrepareMsgType) // check for prepare in case propose came before set the ctrl new height
	case qbft.RoundStateProposal:
		res = msgqueue.SignedMsgIndex(spectypes.SSVConsensusMsgType, identifier, height, specqbft.PrepareMsgType)
	case qbft.RoundStatePrepare:
		res = msgqueue.SignedMsgIndex(spectypes.SSVConsensusMsgType, identifier, height, specqbft.CommitMsgType)
	case qbft.RoundStateCommit:
		return []msgqueue.Index{{}} // qbft.RoundStateCommit stage is NEVER set
	case qbft.RoundStateChangeRound:
		res = msgqueue.SignedMsgIndex(spectypes.SSVConsensusMsgType, identifier, height, specqbft.RoundChangeMsgType)
		//case qbft.RoundStateDecided: needs to pop decided msgs in all cases not only by state
	}
	if len(res) == 0 {
		return []msgqueue.Index{{}}
	}
	return res
}
