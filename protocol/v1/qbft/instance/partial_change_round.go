package instance

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// ChangeRoundPartialQuorumMsgPipeline returns the pipeline which handles partial change ronud quorum
func (i *Instance) ChangeRoundPartialQuorumMsgPipeline() validation.SignedMessagePipeline {
	return i.uponChangeRoundPartialQuorum()
}

// upon receiving a set Frc of f + 1 valid ⟨ROUND-CHANGE, λi, rj, −, −⟩ messages such that:
// 	∀⟨ROUND-CHANGE, λi, rj, −, −⟩ ∈ Frc : rj > ri do
// 		let ⟨ROUND-CHANGE, hi, rmin, −, −⟩ ∈ Frc such that:
// 			∀⟨ROUND-CHANGE, λi, rj, −, −⟩ ∈ Frc : rmin ≤ rj
// 		ri ← rmin
// 		set timer i to running and expire after t(ri)
//		broadcast ⟨ROUND-CHANGE, λi, ri, pri, pvi⟩
func (i *Instance) uponChangeRoundPartialQuorum() validation.SignedMessagePipeline {
	return validation.WrapFunc("upon change round partial quorum", func(_ *message.SignedMessage) error {
		foundPartialQuorum, lowestChangeRound := i.ChangeRoundMessages.PartialChangeRoundQuorum(i.State().GetRound())
		if foundPartialQuorum {
			i.bumpToRound(lowestChangeRound)

			i.Logger.Info("found f+1 change round quorum, bumped round", zap.Uint64("new round", uint64(i.State().GetRound())))
			i.resetRoundTimer()
			i.ProcessStageChange(qbft.RoundState_ChangeRound)

			if err := i.broadcastChangeRound(); err != nil {
				return errors.Wrap(err, "failed finding partial change round quorum")
			}
		}
		return nil
	})
}
