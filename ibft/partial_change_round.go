package ibft

import (
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// ChangeRoundPartialQuorumMsgPipeline returns the pipeline which handles partial change ronud quorum
func (i *Instance) ChangeRoundPartialQuorumMsgPipeline() pipeline.Pipeline {
	return i.uponChangeRoundPartialQuorum()
}

// upon receiving a set Frc of f + 1 valid ⟨ROUND-CHANGE, λi, rj, −, −⟩ messages such that:
// 	∀⟨ROUND-CHANGE, λi, rj, −, −⟩ ∈ Frc : rj > ri do
// 		let ⟨ROUND-CHANGE, hi, rmin, −, −⟩ ∈ Frc such that:
// 			∀⟨ROUND-CHANGE, λi, rj, −, −⟩ ∈ Frc : rmin ≤ rj
// 		ri ← rmin
// 		set timer i to running and expire after t(ri)
//		broadcast ⟨ROUND-CHANGE, λi, ri, pri, pvi⟩
func (i *Instance) uponChangeRoundPartialQuorum() pipeline.Pipeline {
	return pipeline.WrapFunc("upon change round partial quorum", func(_ *proto.SignedMessage) error {
		foundPartialQuorum, lowestChangeRound := i.ChangeRoundMessages.PartialChangeRoundQuorum(i.State.Round.Get())
		if foundPartialQuorum {
			i.State.Round.Set(lowestChangeRound)

			// metrics
			metricsIBFTRound.WithLabelValues(string(i.State.Lambda.Get()),
				i.ValidatorShare.PublicKey.SerializeToHexStr()).Set(float64(lowestChangeRound))

			i.Logger.Info("found f+1 change round quorum, bumped round", zap.Uint64("new round", i.State.Round.Get()))
			i.resetRoundTimer()
			i.ProcessStageChange(proto.RoundState_ChangeRound)

			if err := i.broadcastChangeRound(); err != nil {
				return errors.Wrap(err, "failed finding partial change round quorum")
			}
		}
		return nil
	})
}
