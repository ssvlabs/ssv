package instance

import (
	"bytes"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"

	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/signedmsg"

	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// PrePrepareMsgPipeline is the main pre-prepare msg pipeline
func (i *Instance) PrePrepareMsgPipeline() pipelines.SignedMessagePipeline {
	return pipelines.Combine(
		i.prePrepareMsgValidationPipeline(),
		pipelines.WrapFunc("add pre-prepare msg", func(signedMessage *message.SignedMessage) error {
			i.Logger.Info("received valid pre-prepare message for round",
				zap.Any("sender_ibft_id", signedMessage.GetSigners()),
				zap.Uint64("round", uint64(signedMessage.Message.Round)))

			proposalData, err := signedMessage.Message.GetProposalData()
			if err != nil {
				return err
			}
			i.PrePrepareMessages.AddMessage(signedMessage, proposalData.Data)
			return nil
		}),
		pipelines.CombineQuiet(
			signedmsg.ValidateRound(i.State().GetRound()),
			i.UponPrePrepareMsg(),
		),
	)
}

func (i *Instance) prePrepareMsgValidationPipeline() pipelines.SignedMessagePipeline {
	return i.fork.PrePrepareMsgValidationPipeline(i.ValidatorShare, i.State(), i.RoundLeader)
}

// JustifyPrePrepare implements:
// predicate JustifyPrePrepare(hPRE-PREPARE, λi, round, value)
// 	return
// 		round = 1
// 		∨ received a quorum Qrc of valid <ROUND-CHANGE, λi, round, prj , pvj> messages such that:
// 			∀ <ROUND-CHANGE, λi, round, prj , pvj> ∈ Qrc : prj = ⊥ ∧ prj = ⊥
// 			∨ received a quorum of valid <PREPARE, λi, pr, value> messages such that:
// 				(pr, value) = HighestPrepared(Qrc)
func (i *Instance) JustifyPrePrepare(round uint64, preparedValue []byte) error {
	if round == 1 {
		return nil
	}

	if quorum, _, _ := i.changeRoundQuorum(message.Round(round)); quorum {
		notPrepared, highest, err := i.HighestPrepared(message.Round(round))
		if err != nil {
			return err
		}
		if notPrepared {
			return nil
		} else if !bytes.Equal(preparedValue, highest.PreparedValue) {
			return errors.New("preparedValue different than highest prepared")
		}
		return nil
	}
	return errors.New("no change round quorum")
}

/*
UponPrePrepareMsg Algorithm 2 IBFTController pseudocode for process pi: normal case operation
upon receiving a valid ⟨PRE-PREPARE, λi, ri, value⟩ message m from leader(λi, round) such that:
	JustifyPrePrepare(m) do
		set timer i to running and expire after t(ri)
		broadcast ⟨PREPARE, λi, ri, value⟩
*/
func (i *Instance) UponPrePrepareMsg() pipelines.SignedMessagePipeline {
	return pipelines.WrapFunc("upon pre-prepare msg", func(signedMessage *message.SignedMessage) error {
		// Pre-prepare justification
		err := i.JustifyPrePrepare(uint64(signedMessage.Message.Round), signedMessage.Message.Data)
		if err != nil {
			return errors.Wrap(err, "Unjustified pre-prepare")
		}

		// mark state
		i.ProcessStageChange(qbft.RoundState_PrePrepare)

		// broadcast prepare msg
		prepareMsg, err := signedMessage.Message.GetProposalData()
		if err != nil {
			return errors.Wrap(err, "failed to get prepare message")
		}
		broadcastMsg, err := i.generatePrepareMessage(prepareMsg.Data)
		if err != nil {
			return errors.Wrap(err, "failed to generate prepare message")
		}
		if err := i.SignAndBroadcast(broadcastMsg); err != nil {
			i.Logger.Error("could not broadcast prepare message", zap.Error(err))
			return err
		}
		return nil
	})
}

func (i *Instance) generatePrePrepareMessage(value []byte) (message.ConsensusMessage, error) {
	proposalMsg := &message.ProposalData{
		Data:                     value,
		RoundChangeJustification: nil,
		PrepareJustification:     nil,
	}
	proposalEncodedMsg, err := proposalMsg.Encode()
	if err != nil {
		return message.ConsensusMessage{}, errors.Wrap(err, "failed to encoded proposal message")
	}

	return message.ConsensusMessage{
		MsgType:    message.ProposalMsgType,
		Height:     i.State().GetHeight(),
		Round:      i.State().GetRound(),
		Identifier: i.State().GetIdentifier(),
		Data:       proposalEncodedMsg,
	}, nil
}

func (i *Instance) checkExistingPrePrepare(round message.Round) (bool, *message.SignedMessage, error) {
	msgs := i.PrePrepareMessages.ReadOnlyMessagesByRound(round)
	if len(msgs) == 1 {
		return true, msgs[0], nil
	} else if len(msgs) > 1 {
		return false, nil, errors.New("multiple pre-preparer msgs, can't decide which one to use")
	}
	return false, nil, nil
}
