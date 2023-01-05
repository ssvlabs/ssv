package validator

import (
	"context"
	"fmt"
	"github.com/pkg/errors"
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/patrickmn/go-cache"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v2/ssv/msgqueue"
)

// MessageHandler process the msg. return error if exist
type MessageHandler func(msg *spectypes.SSVMessage) error

// HandleMessage handles a spectypes.SSVMessage.
func (v *Validator) HandleMessage(msg *spectypes.SSVMessage) {
	if q, ok := v.Queues[msg.MsgID.GetRoleType()]; ok {
		q.Add(msg)
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

// ConsumeQueue consumes messages from the msgqueue.Queue of the controller
// it checks for current state
func (v *Validator) ConsumeQueue(msgID spectypes.MessageID, handler MessageHandler, interval time.Duration) error {
	ctx, cancel := context.WithCancel(v.ctx)
	defer cancel()

	q, ok := v.Queues[msgID.GetRoleType()]
	if !ok {
		return errors.New(fmt.Sprintf("queue not found for role %s", msgID.GetRoleType().String()))
	}

	logger := v.logger.With(zap.String("identifier", msgID.String()))
	higherCache := cache.New(time.Second*12, time.Second*24)

	for ctx.Err() == nil {
		time.Sleep(interval)

		// no msg's in the queue
		if q.Len() == 0 {
			// no msg's at all. need to prevent cpu usage in query
			time.Sleep(interval)
			continue
		}
		//// avoid process messages on fork
		// if atomic.LoadUint32(&v.State) == Forking {
		//	time.Sleep(interval)
		//	continue
		//}
		lastHeight := v.GetLastHeight(msgID)
		identifier := msgID.String()

		if processed := v.processHigherHeight(q, handler, identifier, lastHeight, higherCache); processed {
			continue
		}
		if processed := v.processNoRunningInstance(q, handler, msgID, identifier, lastHeight); processed {
			continue
		}
		if processed := v.processByState(q, handler, msgID, identifier, lastHeight); processed {
			continue
		}
		if processed := v.processLateCommit(q, handler, identifier, lastHeight); processed { // nolint:staticcheck
			continue
		}

		// clean all old messages. (when stuck on change round stage, msgs not deleted)
		/*v.Queues.Clean(func(index msgqueue.Index) bool {
			if index.ID != identifier {
				return false
			}
			// remove all msg's that are 2 heights old, besides height 0
			return int64(index.H) <= int64(lastHeight-2) // remove all msg's that are 2 heights old. not post consensus & decided
		})*/
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
	return r.GetBaseRunner().QBFTController.Height
}

// processNoRunningInstance pop msg's only if no current instance running
func (v *Validator) processNoRunningInstance(q msgqueue.MsgQueue, handler MessageHandler, msgID spectypes.MessageID, identifier string, lastHeight specqbft.Height) bool {
	runner := v.DutyRunners.DutyRunnerForMsgID(msgID)
	if runner == nil || (runner.GetBaseRunner().State != nil && runner.GetBaseRunner().State.DecidedValue == nil) {
		return false // only pop when already decided
	}

	logger := v.logger.With(
		// zap.String("sig state", c.SignatureState.getState().toString()),
		zap.Int32("height", int32(lastHeight)))

	iterator := msgqueue.NewIndexIterator().Add(func() msgqueue.Index {
		return msgqueue.SignedPostConsensusMsgIndex(identifier)
	}, func() msgqueue.Index {
		return msgqueue.DecidedMsgIndex(identifier)
	}, func() msgqueue.Index {
		indices := msgqueue.SignedMsgIndex(spectypes.SSVConsensusMsgType, identifier, lastHeight, false, specqbft.CommitMsgType)
		if len(indices) == 0 {
			return msgqueue.Index{}
		}
		return indices[0]
	})

	msgs := q.PopIndices(1, iterator)

	if len(msgs) == 0 || msgs[0] == nil {
		return false // no msg found
	}

	if msgs[0].MsgType == spectypes.SSVConsensusMsgType {
		var sm specqbft.SignedMessage
		if err := sm.Decode(msgs[0].Data); err != nil {
			v.logger.Debug("TestTest processNoRunningInstance error", zap.Error(err))
		} else {
			v.logger.Debug("TestTest processNoRunningInstance", zap.Int32("height", int32(sm.Message.Height)))
		}
	}

	err := handler(msgs[0])
	if err != nil {
		logger.Debug("could not handle message", zap.String("error", err.Error()))
	}
	return true // msg processed
}

// processByState if an instance is running -> get the state and get the relevant messages
func (v *Validator) processByState(q msgqueue.MsgQueue, handler MessageHandler, msgID spectypes.MessageID, identifier string, height specqbft.Height) bool {
	runner := v.DutyRunners.DutyRunnerForMsgID(msgID)
	if !runner.HasRunningDuty() || runner.GetBaseRunner().State.RunningInstance == nil {
		return false
	}
	// currentInstance := v.GetCurrentInstance()
	// if currentInstance == nil {
	//	return false
	//}

	// currentState := currentInstance.GetState()
	msg := v.getNextMsgForState(q, identifier, height)
	if msg == nil {
		return false // no msg found
	}

	if msg.MsgType == spectypes.SSVConsensusMsgType {
		var sm specqbft.SignedMessage
		if err := sm.Decode(msg.Data); err != nil {
			v.logger.Debug("TestTest processByState error", zap.Error(err))
		} else {
			v.logger.Debug("TestTest processByState", zap.Int32("height", int32(sm.Message.Height)))
		}
	}

	err := handler(msg)
	if err != nil {
		v.logger.Debug("could not handle msg", zap.Error(err))
	}
	return true // msg processed
}

// processHigherHeight fetch any message with higher height than last height
func (v *Validator) processHigherHeight(q msgqueue.MsgQueue, handler MessageHandler, identifier string, lastHeight specqbft.Height, higherCache *cache.Cache) bool {
	msgs := q.WithIterator(1, true, func(index msgqueue.Index) bool {
		key := index.String()
		if _, found := higherCache.Get(key); !found {
			higherCache.Set(key, 0, cache.DefaultExpiration)
		} else {
			return false // skip msg
		}

		return index.ID == identifier && index.H > lastHeight
	})

	if len(msgs) > 0 {
		if msgs[0].MsgType == spectypes.SSVConsensusMsgType {
			var sm specqbft.SignedMessage
			if err := sm.Decode(msgs[0].Data); err != nil {
				v.logger.Debug("TestTest processHigherHeight error", zap.Error(err))
			} else {
				v.logger.Debug("TestTest processHigherHeight", zap.Int32("height", int32(sm.Message.Height)))
			}
		}

		err := handler(msgs[0])
		if err != nil {
			v.logger.Debug("could not handle msg", zap.Error(err))
		}
		return true
	}
	return false
}

// processLateCommit this phase is to allow late commit and decided msg's
// we allow late commit and decided up to 1 height back. (only to support pre fork. after fork no need to support previews height)
func (v *Validator) processLateCommit(q msgqueue.MsgQueue, handler MessageHandler, identifier string, lastHeight specqbft.Height) bool {
	iterator := msgqueue.NewIndexIterator().
		Add(func() msgqueue.Index {
			indices := msgqueue.SignedMsgIndex(spectypes.SSVConsensusMsgType, identifier, lastHeight-1, false, specqbft.CommitMsgType)
			if len(indices) == 0 {
				return msgqueue.Index{}
			}
			return indices[0]
		}).
		Add(func() msgqueue.Index {
			indices := msgqueue.SignedMsgIndex(spectypes.SSVConsensusMsgType, identifier, lastHeight-1, true, specqbft.CommitMsgType)
			if len(indices) == 0 {
				return msgqueue.Index{}
			}
			return indices[0]
		})
	msgs := q.PopIndices(1, iterator)

	if len(msgs) > 0 {
		if msgs[0].MsgType == spectypes.SSVConsensusMsgType {
			var sm specqbft.SignedMessage
			if err := sm.Decode(msgs[0].Data); err != nil {
				v.logger.Debug("TestTest processLateCommit error", zap.Error(err))
			} else {
				v.logger.Debug("TestTest processLateCommit", zap.Int32("height", int32(sm.Message.Height)))
			}
		}

		err := handler(msgs[0])
		if err != nil {
			v.logger.Debug("could not handle msg", zap.Error(err))
		}
		return true
	}
	return false
}

// getNextMsgForState return msgs depended on the current instance stage
func (v *Validator) getNextMsgForState(q msgqueue.MsgQueue, identifier string, height specqbft.Height) *spectypes.SSVMessage {
	iterator := msgqueue.NewIndexIterator()

	idxs := msgqueue.SignedMsgIndex(spectypes.SSVConsensusMsgType, identifier, height, false,
		specqbft.ProposalMsgType, specqbft.PrepareMsgType, specqbft.CommitMsgType, specqbft.RoundChangeMsgType)
	for _, idx := range idxs {
		iterator.AddIndex(idx)
	}
	iterator.
		Add(func() msgqueue.Index {
			return msgqueue.DecidedMsgIndex(identifier)
		}) /*.
		Add(func() msgqueue.Index {
			indices := msgqueue.SignedMsgIndex(spectypes.SSVConsensusMsgType, identifier, height, specqbft.RoundChangeMsgType)
			if len(indices) == 0 {
				return msgqueue.Index{}
			}
			return indices[0]
		})*/

	msgs := q.PopIndices(1, iterator)
	if len(msgs) == 0 {
		return nil
	}
	return msgs[0]
}
