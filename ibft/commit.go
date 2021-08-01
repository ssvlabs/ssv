package ibft

import (
	"encoding/hex"
	"errors"
	"github.com/bloxapp/ssv/ibft/pipeline/auth"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/proto"
)

func (i *Instance) commitMsgPipeline() pipeline.Pipeline {
	return pipeline.Combine(
		auth.ValidateRound(i.State.Round),
		i.commitMsgValidationPipeline(),
		pipeline.WrapFunc("add commit msg", func(signedMessage *proto.SignedMessage) error {
			i.Logger.Info("received valid commit message for round",
				zap.String("sender_ibft_id", signedMessage.SignersIDString()),
				zap.Uint64("round", signedMessage.Message.Round))
			i.CommitMessages.AddMessage(signedMessage)
			return nil
		}),
		i.uponCommitMsg(),
	)
}

func (i *Instance) commitMsgValidationPipeline() pipeline.Pipeline {
	return pipeline.Combine(
		auth.BasicMsgValidation(),
		auth.MsgTypeCheck(proto.RoundState_Commit),
		auth.ValidateLambdas(i.State.Lambda),
		auth.ValidateSequenceNumber(i.State.SeqNumber),
		auth.AuthorizeMsg(i.ValidatorShare),
	)
}

func (i *Instance) forceDecidedPipeline() pipeline.Pipeline {
	return pipeline.Combine(
		i.commitMsgValidationPipeline(),
		pipeline.WrapFunc("add commit msg", func(signedMessage *proto.SignedMessage) error {
			i.Logger.Info("received valid decided message for round",
				zap.String("sender_ibft_id", signedMessage.SignersIDString()),
				zap.Uint64("round", signedMessage.Message.Round))
			i.CommitMessages.OverrideMessages(signedMessage)
			return nil
		}),
		i.uponCommitMsg(),
	)
}

// CommittedAggregatedMsg returns a signed message for the state's committed value with the max known signatures
func (i *Instance) CommittedAggregatedMsg() (*proto.SignedMessage, error) {
	if i.State.DecidedMsg != nil {
		return i.State.DecidedMsg, nil
	}
	return nil, errors.New("missing decided message")
}

/**
upon receiving a quorum Qcommit of valid ⟨COMMIT, λi, round, value⟩ messages do:
	set timer i to stopped
	Decide(λi , value, Qcommit)
*/
func (i *Instance) uponCommitMsg() pipeline.Pipeline {
	return pipeline.WrapFunc("upon commit msg", func(signedMessage *proto.SignedMessage) error {
		// check if quorum achieved, act upon it.
		if i.Stage() == proto.RoundState_Decided {
			i.Logger.Info("already commit, not processing commit message")
			return nil // no reason to commit again
		}
		quorum, sigs := i.CommitMessages.QuorumAchieved(signedMessage.Message.Round, signedMessage.Message.Value)
		if quorum {
			i.Logger.Info("commit iBFT instance",
				zap.String("Lambda", hex.EncodeToString(i.State.Lambda)), zap.Uint64("round", i.State.Round),
				zap.Int("got_votes", len(sigs)))

			if aggMsg := i.aggregateMessages(sigs); aggMsg != nil {
				i.State.DecidedMsg = aggMsg
				// mark instance commit
				i.SetStage(proto.RoundState_Decided)
				i.Stop()
			}
		}
		return nil
	})
}

func (i *Instance) aggregateMessages(sigs []*proto.SignedMessage) *proto.SignedMessage {
	var decided *proto.SignedMessage
	var err error
	for _, msg := range sigs {
		if decided == nil {
			decided, err = msg.DeepCopy()
			if err != nil {
				i.Logger.Error("could not copy message")
			}
		} else {
			if err := decided.Aggregate(msg); err != nil {
				i.Logger.Error("could not aggregate message")
			}
		}
	}
	return decided
}

func (i *Instance) generateCommitMessage(value []byte) *proto.Message {
	return &proto.Message{
		Type:      proto.RoundState_Commit,
		Round:     i.State.Round,
		Lambda:    i.State.Lambda,
		SeqNumber: i.State.SeqNumber,
		Value:     value,
	}
}
