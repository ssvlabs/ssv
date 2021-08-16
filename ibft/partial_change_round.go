package ibft

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"go.uber.org/zap"
)

func (i *Instance) findPartialQuorum(msgs []*network.Message) (found bool, lowestChangeRound uint64) {
	lowestChangeRound = uint64(100000) // just a random really large round number
	foundMsgs := make(map[uint64]*proto.SignedMessage)
	quorumCount := 0

	for _, msg := range msgs {
		if err := i.changeRoundMsgValidationPipeline().Run(msg.SignedMessage); err != nil {
			i.Logger.Warn("received invalid change round", zap.Error(err))
			// TODO - how to delete this msg
			continue
		}

		if msg.SignedMessage.Message.Round <= i.State.Round.Get() {
			continue
		}

		for _, signer := range msg.SignedMessage.SignerIds {
			if existingMsg, found := foundMsgs[signer]; found {
				if existingMsg.Message.Round > msg.SignedMessage.Message.Round {
					foundMsgs[signer] = msg.SignedMessage
				}
			} else {
				foundMsgs[signer] = msg.SignedMessage
				quorumCount++
			}

			// recalculate lowest
			if foundMsgs[signer].Message.Round < lowestChangeRound {
				lowestChangeRound = msg.SignedMessage.Message.Round
			}
		}
	}

	minThreshold := i.ValidatorShare.PartialThresholdSize()
	return quorumCount >= minThreshold, lowestChangeRound
}

// upon receiving a set Frc of f + 1 valid ⟨ROUND-CHANGE, λi, rj, −, −⟩ messages such that:
// 	∀⟨ROUND-CHANGE, λi, rj, −, −⟩ ∈ Frc : rj > ri do
// 		let ⟨ROUND-CHANGE, hi, rmin, −, −⟩ ∈ Frc such that:
// 			∀⟨ROUND-CHANGE, λi, rj, −, −⟩ ∈ Frc : rmin ≤ rj
// 		ri ← rmin
// 		set timer i to running and expire after t(ri)
//		broadcast ⟨ROUND-CHANGE, λi, ri, pri, pvi⟩
func (i *Instance) uponChangeRoundPartialQuorum(msgs []*network.Message) (bool, error) {
	//groupedMsgs := i.groupPartialChangeRoundMsgs(msgs)
	foundPartialQuorum, lowestChangeRound := i.findPartialQuorum(msgs)

	// TODO - could have a race condition where msgs are processed in a different thread and then we trigger round change here
	if foundPartialQuorum {
		i.State.Round.Set(lowestChangeRound)
		i.Logger.Info("found f+1 change round quorum, bumped round", zap.Uint64("new round", i.State.Round.Get()))
		i.resetRoundTimer()
		i.ProcessStageChange(proto.RoundState_ChangeRound)

		if err := i.broadcastChangeRound(); err != nil {
			i.Logger.Error("could not broadcast round change message", zap.Error(err))
		}
	}
	return foundPartialQuorum, nil
}
