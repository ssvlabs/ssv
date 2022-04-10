package instance

import (
	"encoding/hex"
	"github.com/bloxapp/ssv/protocol/v1/message"
	ibft2 "github.com/bloxapp/ssv/protocol/v1/qbft"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/signedmsg"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/proto"
)

// ProcessLateCommitMsg tries to aggregate the late commit message to the corresponding decided message
func ProcessLateCommitMsg(msg *message.SignedMessage, qbftStore qbftstorage.QBFTStore, share *message.Share) (*message.SignedMessage, error) {
	logger := logex.GetLogger(zap.String("who", "ProcessLateCommitMsg"),
		zap.Uint64("seq", uint64(msg.Message.Height)), zap.String("identifier", string(msg.Message.Identifier)),
		zap.Any("signers", msg.GetSigners()))
	// find stored decided
	decidedMessages, err := qbftStore.GetDecided(msg.Message.Identifier, msg.Message.Height, msg.Message.Height)
	if err != nil {
		return nil, errors.Wrap(err, "could not read decided for late commit")
	}
	if len(decidedMessages) == 0 {
		// decided message does not exist
		logger.Debug("could not find decided")
		return nil, nil
	}
	decidedMsg := decidedMessages[0]
	if len(decidedMsg.GetSigners()) == share.CommitteeSize() {
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
	if err := qbftStore.SaveDecided(decidedMsg); err != nil {
		return nil, errors.Wrap(err, "could not save aggregated decided message")
	}
	ibft2.ReportDecided(share.PublicKey.SerializeToHexStr(), msg)
	return decidedMsg, nil
}

// CommitMsgPipeline - the main commit msg pipeline
func (i *Instance) CommitMsgPipeline() validation.SignedMessagePipeline {
	return i.fork.CommitMsgPipeline()
}

// CommitMsgPipelineV0 - genesis version 0
func (i *Instance) CommitMsgPipelineV0() validation.SignedMessagePipeline {
	return validation.Combine(
		i.CommitMsgValidationPipeline(),
		validation.WrapFunc("add commit msg", func(signedMessage *message.SignedMessage) error {
			i.Logger.Info("received valid commit message for round",
				zap.Any("sender_ibft_id", signedMessage.GetSigners()),
				zap.Uint64("round", uint64(signedMessage.Message.Round)))
			i.CommitMessages.AddMessage(signedMessage)
			return nil
		}),
		i.uponCommitMsg(),
	)
}

// CommitMsgValidationPipeline is the main commit msg pipeline
func (i *Instance) CommitMsgValidationPipeline() validation.SignedMessagePipeline {
	return i.fork.CommitMsgValidationPipeline()
}

// CommitMsgValidationPipelineV0 is version 0
func (i *Instance) CommitMsgValidationPipelineV0() validation.SignedMessagePipeline {
	return CommitMsgValidationPipelineV0(i.State().GetIdentifier(), i.State().GetHeight(), i.ValidatorShare)
}

// CommitMsgValidationPipelineV0 is version 0 of commit message validation
func CommitMsgValidationPipelineV0(identifier message.Identifier, seq message.Height, share *message.Share) validation.SignedMessagePipeline {
	return validation.Combine(
		signedmsg.BasicMsgValidation(),
		signedmsg.MsgTypeCheck(message.CommitMsgType),
		signedmsg.ValidateLambdas(identifier),
		signedmsg.ValidateSequenceNumber(seq),
		signedmsg.AuthorizeMsg(share),
	)
}

// DecidedMsgPipeline is the main pipeline for decided msgs
func (i *Instance) DecidedMsgPipeline() validation.SignedMessagePipeline {
	return i.fork.DecidedMsgPipeline()
}

// DecidedMsgPipelineV0 is version 0
func (i *Instance) DecidedMsgPipelineV0() validation.SignedMessagePipeline {
	return validation.Combine(
		i.CommitMsgValidationPipeline(),
		validation.WrapFunc("add commit msg", func(signedMessage *message.SignedMessage) error {
			i.Logger.Info("received valid decided message for round",
				zap.Any("sender_ibft_id", signedMessage.GetSigners()),
				zap.Uint64("round", uint64(signedMessage.Message.Round)))
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
func (i *Instance) uponCommitMsg() validation.SignedMessagePipeline {
	return validation.WrapFunc("upon commit msg", func(signedMessage *message.SignedMessage) error {
		quorum, sigs := i.CommitMessages.QuorumAchieved(signedMessage.Message.Round, signedMessage.Message.Data)
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
