package ibft

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"go.uber.org/zap"
)

func (i *Instance) changeRoundPartialQuorum(msgs []*proto.SignedMessage) (quorum bool, t int, n int) {
	// TODO - calculate quorum one way (for prepare, commit, change round and decided) and refactor
	// TODO - refactor a way to calculate partial quorum
	quorum = len(msgs)*3 >= i.ValidatorShare.CommitteeSize()*1
	return quorum, len(msgs), i.ValidatorShare.CommitteeSize()
}

func (i *Instance) findPartialQuorum(msgs []*network.Message) (found bool, lowestChangeRound uint64) {
	lowestChangeRound = uint64(100000) // just a random really large round number
	relevantMsgs := make([]*proto.SignedMessage, 0)
	for _, msg := range msgs {
		if err := i.changeRoundMsgValidationPipeline().Run(msg.SignedMessage); err != nil {
			i.Logger.Warn("received invalid change round", zap.Error(err))
			// TODO - how to delete this msg
			continue
		}

		if msg.SignedMessage.Message.Round <= i.State.Round {
			continue
		}
		relevantMsgs = append(relevantMsgs, msg.SignedMessage)
		if msg.SignedMessage.Message.Round < lowestChangeRound {
			lowestChangeRound = msg.SignedMessage.Message.Round
		}
	}

	found, _, _ = i.changeRoundPartialQuorum(relevantMsgs)

	return found, lowestChangeRound
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
		i.State.Round = lowestChangeRound
		i.resetRoundTimer()

		if err := i.broadcastChangeRound(); err != nil {
			i.Logger.Error("could not broadcast round change message", zap.Error(err))
		}
	}
	return foundPartialQuorum, nil
}
