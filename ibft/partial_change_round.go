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

func (i *Instance) groupPartialChangeRoundMsgs(msgs []*network.Message) map[uint64][]*proto.SignedMessage {
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
	return groupedMsgs
}

func (i *Instance) findPartialQuorum(groupedMsgs map[uint64][]*proto.SignedMessage) (foundPartialQuorum bool, lowestChangeRound uint64) {
	lowestChangeRound = uint64(100000) // just a random really large round number
	foundPartialQuorum = false
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
	return foundPartialQuorum, lowestChangeRound
}

// upon receiving a set Frc of f + 1 valid ⟨ROUND-CHANGE, λi, rj, −, −⟩ messages such that:
// 	∀⟨ROUND-CHANGE, λi, rj, −, −⟩ ∈ Frc : rj > ri do
// 		let ⟨ROUND-CHANGE, hi, rmin, −, −⟩ ∈ Frc such that:
// 			∀⟨ROUND-CHANGE, λi, rj, −, −⟩ ∈ Frc : rmin ≤ rj
// 		ri ← rmin
// 		set timer i to running and expire after t(ri)
//		broadcast ⟨ROUND-CHANGE, λi, ri, pri, pvi⟩
func (i *Instance) uponChangeRoundPartialQuorum(msgs []*network.Message) (bool, error) {
	groupedMsgs := i.groupPartialChangeRoundMsgs(msgs)
	foundPartialQuorum, lowestChangeRound := i.findPartialQuorum(groupedMsgs)

	// TODO - could have a race condition where msgs are processed in a different thread and then we trigger round change here
	if foundPartialQuorum {
		i.stopRoundChangeTimer()
		i.State.Round = lowestChangeRound
		i.triggerRoundChangeOnTimer()

		if err := i.broadcastChangeRound(); err != nil {
			i.Logger.Error("could not broadcast round change message", zap.Error(err))
		}
	}
	return foundPartialQuorum, nil
}
