package ibft

import (
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network/msgqueue"
	"go.uber.org/zap"
)

// ProcessMessage pulls messages from the queue to be processed sequentially
func (i *Instance) ProcessMessage() bool {
	if netMsg := i.MsgQueue.PopMessage(msgqueue.IBFTRoundIndexKey(i.State.Lambda, i.State.Round)); netMsg != nil {
		var pp pipeline.Pipeline
		switch netMsg.Msg.Message.Type {
		case proto.RoundState_PrePrepare:
			pp = i.prePrepareMsgPipeline()
		case proto.RoundState_Prepare:
			pp = i.prepareMsgPipeline()
		case proto.RoundState_Commit:
			pp = i.commitMsgPipeline()
		case proto.RoundState_ChangeRound:
			pp = i.changeRoundMsgPipeline()
		default:
			i.Logger.Warn("undefined message type", zap.Any("msg", netMsg.Msg))
			return true
		}

		if err := pp.Run(netMsg.Msg); err != nil {
			i.Logger.Error("msg pipeline error", zap.Error(err))
		}
		return true
	}
	return false
}
