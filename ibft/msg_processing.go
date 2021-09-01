package ibft

import (
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/pipeline/changeround"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network/msgqueue"
	"go.uber.org/zap"
)

// ProcessMessage pulls messages from the queue to be processed sequentially
func (i *Instance) ProcessMessage() (processedMsg bool, err error) {
	if netMsg := i.MsgQueue.PopMessage(msgqueue.IBFTMessageIndexKey(i.State.Lambda.Get(), i.State.SeqNumber.Get())); netMsg != nil {
		var pp pipeline.Pipeline
		switch netMsg.SignedMessage.Message.Type {
		case proto.RoundState_PrePrepare:
			pp = i.prePrepareMsgPipeline()
		case proto.RoundState_Prepare:
			pp = i.prepareMsgPipeline()
		case proto.RoundState_Commit:
			pp = i.commitMsgPipeline()
		case proto.RoundState_ChangeRound:
			pp = pipeline.Combine(
				i.changeRoundMsgValidationPipeline(),
				changeround.AddChangeRoundMessage(i.Logger, i.ChangeRoundMessages, i.State),
				i.ChangeRoundPartialQuorumMsgPipeline(),
				i.changeRoundFullQuorumMsgPipeline(),
			)
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
