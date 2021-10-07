package ibft

import (
	"encoding/hex"
	"github.com/bloxapp/ssv/ibft/pipeline/auth"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/pkg/errors"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/proto"
)

// ProcessLateCommitMsg tries to aggregate the late commit message to the corresponding decided message
func ProcessLateCommitMsg(msg *proto.SignedMessage, ibftStorage collections.Iibft, pubkey string) (bool, error) {
	// find stored decided
	decidedMsg, found, err := ibftStorage.GetDecided(msg.Message.Lambda, msg.Message.SeqNumber)
	if err != nil {
		return false, errors.Wrap(err, "could not fetch decided for late commit")
	}
	if !found {
		return false, nil
	}
	// aggregate message with stored decided
	if err := decidedMsg.Aggregate(msg); err != nil {
		if err == proto.ErrDuplicateMsgSigner {
			return false, nil
		}
		return false, errors.Wrap(err, "could not aggregate commit message")
	}
	// save to storage
	if err := ibftStorage.SaveDecided(decidedMsg); err != nil {
		return false, errors.Wrap(err, "could not save aggregated decided message")
	}
	ReportDecided(pubkey, msg)
	return true, nil
}

func (i *Instance) commitMsgPipeline() pipeline.Pipeline {
	return pipeline.Combine(
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
		auth.ValidateLambdas(i.State.Lambda.Get()),
		auth.ValidateSequenceNumber(i.State.SeqNumber.Get()),
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
	if i.State == nil {
		return nil, errors.New("missing instance state")
	}
	if i.decidedMsg != nil {
		return i.decidedMsg, nil
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
		quorum, sigs := i.CommitMessages.QuorumAchieved(signedMessage.Message.Round, signedMessage.Message.Value)
		if quorum {
			i.processCommitQuorumOnce.Do(func() {
				i.Logger.Info("commit iBFT instance",
					zap.String("Lambda", hex.EncodeToString(i.State.Lambda.Get())), zap.Uint64("round", i.State.Round.Get()),
					zap.Int("got_votes", len(sigs)))

				aggMsg, err := proto.AggregateMessages(sigs)
				if err != nil {
					i.Logger.Error("could not aggregate commit messages after quorum", zap.Error(err))
				}
				i.decidedMsg = aggMsg
				// mark instance commit
				i.ProcessStageChange(proto.RoundState_Decided)
				i.Stop()
			})
		}
		return nil
	})
}

func (i *Instance) generateCommitMessage(value []byte) *proto.Message {
	return &proto.Message{
		Type:      proto.RoundState_Commit,
		Round:     i.State.Round.Get(),
		Lambda:    i.State.Lambda.Get(),
		SeqNumber: i.State.SeqNumber.Get(),
		Value:     value,
	}
}
