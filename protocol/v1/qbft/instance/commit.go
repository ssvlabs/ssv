package instance

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/signedmsg"
)

// CommitMsgPipeline - the main commit msg pipeline
func (i *Instance) CommitMsgPipeline() pipelines.SignedMessagePipeline {
	return pipelines.Combine(
		i.CommitMsgValidationPipeline(),
		pipelines.WrapFunc("add commit msg", func(signedMessage *specqbft.SignedMessage) error {
			i.Logger.Info("received valid commit message for round",
				zap.Any("sender_ibft_id", signedMessage.GetSigners()),
				zap.Uint64("round", uint64(signedMessage.Message.Round)))

			commitData, err := signedMessage.Message.GetCommitData()
			if err != nil {
				return err
			}
			i.containersMap[specqbft.CommitMsgType].AddMessage(signedMessage, commitData.Data)
			return nil
		}),
		pipelines.CombineQuiet(
			signedmsg.ValidateRound(i.State().GetRound()),
			i.uponCommitMsg(),
		),
	)
}

// CommitMsgValidationPipeline is the main commit msg pipeline
func (i *Instance) CommitMsgValidationPipeline() pipelines.SignedMessagePipeline {
	return i.fork.CommitMsgValidationPipeline(i.ValidatorShare, i.State().GetIdentifier(), i.State().GetHeight())
}

// DecidedMsgPipeline is the main pipeline for decided msgs
func (i *Instance) DecidedMsgPipeline() pipelines.SignedMessagePipeline {
	return pipelines.Combine(
		i.CommitMsgValidationPipeline(),
		pipelines.WrapFunc("add commit msg", func(signedMessage *specqbft.SignedMessage) error {
			i.Logger.Info("received valid decided message for round",
				zap.Any("sender_ibft_id", signedMessage.GetSigners()),
				zap.Uint64("round", uint64(signedMessage.Message.Round)))

			commitData, err := signedMessage.Message.GetCommitData()
			if err != nil {
				return err
			}
			i.containersMap[specqbft.CommitMsgType].OverrideMessages(signedMessage, commitData.Data)
			return nil
		}),
		i.uponCommitMsg(),
	)
}

/**
upon receiving a quorum Qcommit of valid ⟨COMMIT, λi, round, value⟩ messages do:
	set timer i to stopped
	Decide(λi , value, Qcommit)
*/
func (i *Instance) uponCommitMsg() pipelines.SignedMessagePipeline {
	return pipelines.WrapFunc("upon commit msg", func(signedMessage *specqbft.SignedMessage) error {
		msgCommitData, err := signedMessage.Message.GetCommitData()
		if err != nil {
			return errors.Wrap(err, "failed to get commit data")
		}
		quorum, sigs := i.containersMap[specqbft.CommitMsgType].QuorumAchieved(signedMessage.Message.Round, msgCommitData.Data)
		if quorum {
			i.processCommitQuorumOnce.Do(func() {
				i.Logger.Info("commit iBFT instance",
					zap.String("Lambda", i.State().GetIdentifier().String()), zap.Uint64("round", uint64(i.State().GetRound())),
					zap.Int("got_votes", len(sigs)))

				// need to cant signedMessages to message.MsgSignature TODO other way? (:Niv)
				var msgSig []spectypes.MessageSignature
				for _, s := range sigs {
					msgSig = append(msgSig, s)
				}

				aggMsg := sigs[0].DeepCopy()
				for _, s := range msgSig[1:] {
					if err := aggMsg.Aggregate(s); err != nil {
						i.Logger.Error("could not aggregate commit messages after quorum", zap.Error(err)) //TODO need to return?
					}
				}

				i.decidedMsg = aggMsg
				// mark instance commit
				i.ProcessStageChange(qbft.RoundStateDecided)
			})
		}
		return nil
	})
}

func (i *Instance) generateCommitMessage(value []byte) (*specqbft.Message, error) {
	commitMsg := &specqbft.CommitData{Data: value}
	encodedCommitMsg, err := commitMsg.Encode()
	if err != nil {
		return nil, err
	}
	identifier := i.State().GetIdentifier()
	return &specqbft.Message{
		MsgType:    specqbft.CommitMsgType,
		Height:     i.State().GetHeight(),
		Round:      i.State().GetRound(),
		Identifier: identifier[:],
		Data:       encodedCommitMsg,
	}, nil
}
