package ibft

import (
	"encoding/hex"
	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/ibft/pipeline/auth"
	"github.com/bloxapp/ssv/protocol/v1/validator/types"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/proto"
)

// ProcessLateCommitMsg tries to aggregate the late commit message to the corresponding decided message
func ProcessLateCommitMsg(msg *proto.SignedMessage, ibftStorage collections.Iibft, share *types.Share) (*proto.SignedMessage, error) {
	logger := logex.GetLogger(zap.String("who", "ProcessLateCommitMsg"),
		zap.Uint64("seq", msg.Message.SeqNumber), zap.String("identifier", string(msg.Message.Lambda)),
		zap.Uint64s("signers", msg.SignerIds))
	// find stored decided
	decidedMsg, found, err := ibftStorage.GetDecided(msg.Message.Lambda, msg.Message.SeqNumber)
	if err != nil {
		return nil, errors.Wrap(err, "could not read decided for late commit")
	}
	if !found {
		// decided message does not exist
		logger.Debug("could not find decided")
		return nil, nil
	}
	if len(decidedMsg.SignerIds) == share.CommitteeSize() {
		// msg was signed by the entire committee
		logger.Debug("msg was signed by the entire committee")
		return nil, nil
	}
	// aggregate message with stored decided
	if err := decidedMsg.Aggregate(msg); err != nil {
		if err == proto.ErrDuplicateMsgSigner {
			logger.Debug("duplicated signer")
			return nil, nil
		}
		return nil, errors.Wrap(err, "could not aggregate commit message")
	}
	if err := ibftStorage.SaveDecided(decidedMsg); err != nil {
		return nil, errors.Wrap(err, "could not save aggregated decided message")
	}
	ibft.ReportDecided(share.PublicKey.SerializeToHexStr(), msg)
	return decidedMsg, nil
}

// CommitMsgPipeline - the main commit msg pipeline
func (i *Instance) CommitMsgPipeline() pipeline.Pipeline {
	return i.fork.CommitMsgPipeline()
}

// CommitMsgPipelineV0 - genesis version 0
func (i *Instance) CommitMsgPipelineV0() pipeline.Pipeline {
	return pipeline.Combine(
		i.CommitMsgValidationPipeline(),
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

// CommitMsgValidationPipeline is the main commit msg pipeline
func (i *Instance) CommitMsgValidationPipeline() pipeline.Pipeline {
	return i.fork.CommitMsgValidationPipeline()
}

// CommitMsgValidationPipelineV0 is version 0
func (i *Instance) CommitMsgValidationPipelineV0() pipeline.Pipeline {
	return CommitMsgValidationPipelineV0(i.State().Lambda.Get(), i.State().SeqNumber.Get(), i.ValidatorShare)
}

// CommitMsgValidationPipelineV0 is version 0 of commit message validation
func CommitMsgValidationPipelineV0(identifier []byte, seq uint64, share *types.Share) pipeline.Pipeline {
	return pipeline.Combine(
		auth.BasicMsgValidation(),
		auth.MsgTypeCheck(proto.RoundState_Commit),
		auth.ValidateLambdas(identifier),
		auth.ValidateSequenceNumber(seq),
		auth.AuthorizeMsg(share),
	)
}

// DecidedMsgPipeline is the main pipeline for decided msgs
func (i *Instance) DecidedMsgPipeline() pipeline.Pipeline {
	return i.fork.DecidedMsgPipeline()
}

// DecidedMsgPipelineV0 is version 0
func (i *Instance) DecidedMsgPipelineV0() pipeline.Pipeline {
	return pipeline.Combine(
		i.CommitMsgValidationPipeline(),
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
					zap.String("Lambda", hex.EncodeToString(i.State().Lambda.Get())), zap.Uint64("round", i.State().Round.Get()),
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
		Round:     i.State().Round.Get(),
		Lambda:    i.State().Lambda.Get(),
		SeqNumber: i.State().SeqNumber.Get(),
		Value:     value,
	}
}
