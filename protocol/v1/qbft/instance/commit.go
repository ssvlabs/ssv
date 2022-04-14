package instance

import (
	"encoding/hex"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	qbft "github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/signedmsg"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/proto"
)

// ProcessLateCommitMsg tries to aggregate the late commit message to the corresponding decided message
func ProcessLateCommitMsg(msg *message.SignedMessage, qbftStore qbftstorage.QBFTStore, share *beacon.Share) (*message.SignedMessage, error) {
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
	qbft.ReportDecided(share.PublicKey.SerializeToHexStr(), msg)
	return decidedMsg, nil
}

// CommitMsgPipeline - the main commit msg pipeline
func (i *Instance) CommitMsgPipeline() pipelines.SignedMessagePipeline {
	return pipelines.Combine(
		i.CommitMsgValidationPipeline(),
		pipelines.WrapFunc("add commit msg", func(signedMessage *message.SignedMessage) error {
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

				aggMsg := sigs[0]
				if err := aggMsg.Aggregate(msgSig...); err != nil {
					i.Logger.Error("could not aggregate commit messages after quorum", zap.Error(err))
				}

				i.decidedMsg = aggMsg
				// mark instance commit
				i.ProcessStageChange(qbft.RoundState_Decided)
			})
		}
		return nil
	})
}

func (i *Instance) generateCommitMessage(value []byte) *message.ConsensusMessage {
	return &message.ConsensusMessage{
		MsgType:    message.CommitMsgType,
		Height:     i.State().GetHeight(),
		Round:      i.State().GetRound(),
		Identifier: i.State().GetIdentifier(),
		Data:       value,
	}
}
