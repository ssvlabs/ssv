package instance

import (
	"encoding/hex"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/signedmsg"
)

// CommitMsgPipeline - the main commit msg pipeline
func (i *Instance) CommitMsgPipeline() pipelines.SignedMessagePipeline {
	return pipelines.Combine(
		i.CommitMsgValidationPipeline(),
		pipelines.WrapFunc("add commit msg", func(signedMessage *message.SignedMessage) error {
			i.Logger.Info("received valid commit message for round",
				zap.Any("sender_ibft_id", signedMessage.GetSigners()),
				zap.Uint64("round", uint64(signedMessage.Message.Round)))

			commitData, err := signedMessage.Message.GetCommitData()
			if err != nil {
				return err
			}
			i.CommitMessages.AddMessage(signedMessage, commitData.Data)
			return nil
		}),
		i.uponCommitMsg(),
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
		pipelines.WrapFunc("add commit msg", func(signedMessage *message.SignedMessage) error {
			i.Logger.Info("received valid decided message for round",
				zap.Any("sender_ibft_id", signedMessage.GetSigners()),
				zap.Uint64("round", uint64(signedMessage.Message.Round)))

			commitData, err := signedMessage.Message.GetCommitData()
			if err != nil {
				return err
			}
			i.CommitMessages.OverrideMessages(signedMessage, commitData.Data)
			return nil
		}),
		pipelines.CombineQuiet(
			signedmsg.ValidateRound(i.State().GetRound()),
			i.uponCommitMsg(),
		),
	)
}

/**
upon receiving a quorum Qcommit of valid ⟨COMMIT, λi, round, value⟩ messages do:
	set timer i to stopped
	Decide(λi , value, Qcommit)
*/
func (i *Instance) uponCommitMsg() pipelines.SignedMessagePipeline {
	return pipelines.WrapFunc("upon commit msg", func(signedMessage *message.SignedMessage) error {
		msgCommitData, err := signedMessage.Message.GetCommitData()
		if err != nil {
			return errors.Wrap(err, "failed to get commit data")
		}
		quorum, sigs := i.CommitMessages.QuorumAchieved(signedMessage.Message.Round, msgCommitData.Data)
		if quorum {
			i.processCommitQuorumOnce.Do(func() {
				i.Logger.Info("commit iBFT instance",
					zap.String("Lambda", hex.EncodeToString(i.State().GetIdentifier())), zap.Uint64("round", uint64(i.State().GetRound())),
					zap.Int("got_votes", len(sigs)))

				// need to cant signedMessages to message.MsgSignature TODO other way? (:Niv)
				var msgSig []message.MsgSignature
				for _, s := range sigs {
					msgSig = append(msgSig, s)
				}

				aggMsg := sigs[0].DeepCopy()
				if err := aggMsg.Aggregate(msgSig[1:]...); err != nil {
					i.Logger.Error("could not aggregate commit messages after quorum", zap.Error(err)) //TODO need to return?
				}

				i.decidedMsg = aggMsg
				// mark instance commit
				i.ProcessStageChange(qbft.RoundStateDecided)
			})
		}
		return nil
	})
}

func (i *Instance) generateCommitMessage(value []byte) (*message.ConsensusMessage, error) {
	commitMsg := &message.CommitData{Data: value}
	encodedCommitMsg, err := commitMsg.Encode()
	if err != nil {
		return nil, err
	}
	return &message.ConsensusMessage{
		MsgType:    message.CommitMsgType,
		Height:     i.State().GetHeight(),
		Round:      i.State().GetRound(),
		Identifier: i.State().GetIdentifier(),
		Data:       encodedCommitMsg,
	}, nil
}
