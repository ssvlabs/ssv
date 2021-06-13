package ibft

import (
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network/msgqueue"
	"go.uber.org/zap"
)

// ProcessMessage pulls messages from the queue to be processed sequentially
func (i *Instance) ProcessMessage() (processedMsg bool, err error) {
	if netMsg := i.MsgQueue.PopMessage(msgqueue.IBFTRoundIndexKey(i.State.Lambda, i.State.Round)); netMsg != nil {
		var pp pipeline.Pipeline
		switch netMsg.SignedMessage.Message.Type {
		case proto.RoundState_PrePrepare:
			pp = i.prePrepareMsgPipeline()
		case proto.RoundState_Prepare:
			pp = i.prepareMsgPipeline()
		case proto.RoundState_Commit:
			pp = i.commitMsgPipeline()
		case proto.RoundState_ChangeRound:
			pp = i.changeRoundMsgPipeline()
		default:
			i.Logger.Warn("undefined message type", zap.Any("msg", netMsg.SignedMessage))
			return true, nil
		}

		i.msgProcessingLock.Lock()
		defer i.msgProcessingLock.Unlock()
		if err := pp.Run(netMsg.SignedMessage); err != nil {
			return true, err
		}
		return true, nil
	}
	return false, nil
}

// upon receiving a set Frc of f + 1 valid ⟨ROUND-CHANGE, λi, rj, −, −⟩ messages such that:
// 	∀⟨ROUND-CHANGE, λi, rj, −, −⟩ ∈ Frc : rj > ri do
// 		let ⟨ROUND-CHANGE, hi, rmin, −, −⟩ ∈ Frc such that:
// 			∀⟨ROUND-CHANGE, λi, rj, −, −⟩ ∈ Frc : rmin ≤ rj
// 		ri ← rmin
// 		set timer i to running and expire after t(ri)
//		broadcast ⟨ROUND-CHANGE, λi, ri, pri, pvi⟩
func (i *Instance) ProcessChangeRoundPartialQuorum() (found bool, err error) {
	if msgs := i.MsgQueue.MessagesForIndex(msgqueue.IBFTAllRoundChangeIndexKey(i.State.Lambda)); msgs != nil {
		groupedMsgs := make(map[uint64][]*proto.SignedMessage)
		// validate and group change round messages
		for _, msg := range msgs {
			if err := i.changeRoundMsgValidationPipeline().Run(msg.SignedMessage); err == nil {
				if _, found := groupedMsgs[msg.SignedMessage.Message.Round]; !found {
					groupedMsgs[msg.SignedMessage.Message.Round] = make([]*proto.SignedMessage, 0)
				}
				groupedMsgs[msg.SignedMessage.Message.Round] = append(groupedMsgs[msg.SignedMessage.Message.Round], msg.SignedMessage)
			}
		}

		// find f+1 msgs such that their round is min + higher than state round
		lowestChangeRound := uint64(100000) // just a random really large round number
		foundPartialQuorum := false
		for round, msgs := range groupedMsgs {
			if round <= i.State.Round {
				continue
			}
			if found, _, _ := i.changeRoundPartialQuorum(msgs); found {
				foundPartialQuorum = true
				if round < lowestChangeRound {
					lowestChangeRound = round
				}
			}
		}

		// if found, bump round to lowest + trigger set timer
		if foundPartialQuorum {
			i.msgProcessingLock.Lock()
			defer i.msgProcessingLock.Unlock()

			i.stopRoundChangeTimer()
			// TODO - purge all round change msgs?
			i.State.Round = lowestChangeRound
			i.startRoundChangeOnTimer()

			// TODO - broadcast round change

			return true, nil
		}

	}
	return false, nil
}
