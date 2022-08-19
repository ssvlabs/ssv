package instance

import (
	"bytes"
	"encoding/hex"
	"fmt"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/msgcont"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/signedmsg"
)

// CommitMsgPipeline - the main commit msg pipeline
func (i *Instance) CommitMsgPipeline() pipelines.SignedMessagePipeline {
	validationPipeline := i.CommitMsgValidationPipeline()
	return pipelines.Combine(
		signedmsg.ProposalExists(i.State()),
		pipelines.WrapFunc(validationPipeline.Name(), func(signedMessage *specqbft.SignedMessage) error {
			if err := validationPipeline.Run(signedMessage); err != nil {
				return fmt.Errorf("invalid commit message: %w", err)
			}
			return nil
		}),

		i.uponCommitMsg(),
	)
}

// CommitMsgValidationPipeline is the commit msg validation pipeline.
func (i *Instance) CommitMsgValidationPipeline() pipelines.SignedMessagePipeline {
	return pipelines.Combine(
		i.fork.CommitMsgValidationPipeline(i.ValidatorShare, i.State()),
		pipelines.WrapFunc("add commit msg", func(signedMessage *specqbft.SignedMessage) error {
			i.Logger.Info("received valid commit message for round",
				zap.Any("sender_ibft_id", signedMessage.GetSigners()),
				zap.Uint64("round", uint64(signedMessage.Message.Round)))

			commitData, err := signedMessage.Message.GetCommitData()
			if err != nil {
				return fmt.Errorf("could not get msg commit data: %w", err)
			}
			i.containersMap[specqbft.CommitMsgType].AddMessage(signedMessage, commitData.Data)
			return nil
		}),
	)
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
		quorum, commitMsgs, err := commitQuorumForCurrentRoundValue(i.State(), i.ValidatorShare, i.containersMap[specqbft.CommitMsgType], signedMessage.Message.Data)
		if err != nil {
			return fmt.Errorf("could not calculate commit quorum: %w", err)
		}
		if !quorum {
			return nil
		}

		var onceErr error
		i.processCommitQuorumOnce.Do(func() {
			i.Logger.Info("commit iBFT instance",
				zap.String("Lambda", hex.EncodeToString(i.State().GetIdentifier())),
				zap.Uint64("round", uint64(i.State().GetRound())),
				zap.Int("got_votes", len(commitMsgs)))

			agg, err := aggregateCommitMsgs(commitMsgs)
			if err != nil {
				onceErr = fmt.Errorf("could not aggregate commit msgs: %w", err)
				return
			}

			i.decidedMsg = agg
			// mark instance commit
			i.ProcessStageChange(qbft.RoundStateDecided)
		})

		return onceErr
	})
}

func aggregateCommitMsgs(msgs []*specqbft.SignedMessage) (*specqbft.SignedMessage, error) {
	if len(msgs) == 0 {
		return nil, fmt.Errorf("can't aggregate zero commit msgs")
	}

	var ret *specqbft.SignedMessage
	for _, m := range msgs {
		if ret == nil {
			ret = m.DeepCopy()
		} else {
			if err := ret.Aggregate(m); err != nil {
				return nil, fmt.Errorf("could not aggregate commit msg: %w", err)
			}
		}
	}
	return ret, nil
}

// returns true if there is a quorum for the current round for this provided value
func commitQuorumForCurrentRoundValue(state *qbft.State, share *beacon.Share, commitMsgContainer msgcont.MessageContainer, value []byte) (bool, []*specqbft.SignedMessage, error) {
	signers, msgs := longestUniqueSignersForRoundAndValue(commitMsgContainer, state.GetRound(), value)
	return len(signers)*3 >= share.CommitteeSize()*2, msgs, nil
}

func longestUniqueSignersForRoundAndValue(container msgcont.MessageContainer, round specqbft.Round, value []byte) ([]spectypes.OperatorID, []*specqbft.SignedMessage) {
	signersRet := make([]spectypes.OperatorID, 0)
	msgsRet := make([]*specqbft.SignedMessage, 0)
	messagesByRound := container.ReadOnlyMessagesByRound(round)

	if messagesByRound == nil {
		return signersRet, msgsRet
	}

	for i := 0; i < len(messagesByRound); i++ {
		m := messagesByRound[i]

		if !bytes.Equal(m.Message.Data, value) {
			continue
		}

		currentSigners := make([]spectypes.OperatorID, 0)
		currentMsgs := make([]*specqbft.SignedMessage, 0)
		currentMsgs = append(currentMsgs, m)
		currentSigners = append(currentSigners, m.GetSigners()...)
		for j := i + 1; j < len(messagesByRound); j++ {
			m2 := messagesByRound[j]

			if !bytes.Equal(m2.Message.Data, value) {
				continue
			}

			if !m2.CommonSigners(currentSigners) {
				currentMsgs = append(currentMsgs, m2)
				currentSigners = append(currentSigners, m2.GetSigners()...)
			}
		}

		if len(signersRet) < len(currentSigners) {
			signersRet = currentSigners
			msgsRet = currentMsgs
		}
	}

	return signersRet, msgsRet
}

func (i *Instance) generateCommitMessage(value []byte) (*specqbft.Message, error) {
	commitMsg := &specqbft.CommitData{Data: value}
	encodedCommitMsg, err := commitMsg.Encode()
	if err != nil {
		return nil, err
	}

	return &specqbft.Message{
		MsgType:    specqbft.CommitMsgType,
		Height:     i.State().GetHeight(),
		Round:      i.State().GetRound(),
		Identifier: i.State().GetIdentifier(),
		Data:       encodedCommitMsg,
	}, nil
}
