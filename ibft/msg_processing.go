package ibft

import (
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/msgqueue"
	"go.uber.org/zap"
)

// ProcessMessage pulls messages from the queue to be processed sequentially
func (i *Instance) ProcessMessage() (processedMsg bool, err error) {
	i.msgProcessingLock.Lock()
	defer i.msgProcessingLock.Unlock()
	if netMsg := i.MsgQueue.PopMessage(msgqueue.IBFTRoundIndexKey(i.State.Lambda, i.State.SeqNumber, i.State.Round)); netMsg != nil {
		var pp pipeline.Pipeline
		switch netMsg.SignedMessage.Message.Type {
		case proto.RoundState_PrePrepare:
			pp = i.prePrepareMsgPipeline()
		case proto.RoundState_Prepare:
			pp = i.prepareMsgPipeline()
		case proto.RoundState_Commit:
			pp = i.commitMsgPipeline()
		case proto.RoundState_ChangeRound:
			pp = i.changeRoundFullQuorumMsgPipeline()
		default:
			i.Logger.Warn("undefined message type", zap.Any("msg", netMsg.SignedMessage))
			return true, nil
		}
		if err := pp.Run(netMsg.SignedMessage); err != nil {
			return true, err
		}
		return true, nil
	}
	return false, nil
}

// ProcessChangeRoundPartialQuorum will look for f+1 change round msgs to bump to a higher round if this instance is behind.
func (i *Instance) ProcessChangeRoundPartialQuorum() (found bool, err error) {
	i.msgProcessingLock.Lock()
	defer i.msgProcessingLock.Unlock()
	if msgsMap := i.MsgQueue.MessagesForIndex(msgqueue.IBFTAllRoundChangeIndexKey(i.State.Lambda)); msgsMap != nil {
		// get values and keys slices
		msgs := make([]*network.Message, 0)
		msgsIds := make([]string, 0)
		for id, msg := range msgsMap {
			msgs = append(msgs, msg)
			msgsIds = append(msgsIds, id)
		}

		found, err := i.uponChangeRoundPartialQuorum(msgs)
		if err != nil {
			return false, err
		}
		if found {
			// We delete all change round messages as we already acted upon them so they have no use.
			i.MsgQueue.DeleteMessagesWithIds(msgsIds)
		}
		return found, err
	}
	return false, nil
}
