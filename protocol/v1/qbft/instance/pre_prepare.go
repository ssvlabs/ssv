package instance

import (
	"bytes"

	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/preprepare"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/signed_msg"

	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// PrePrepareMsgPipeline is the main pre-prepare msg pipeline
func (i *Instance) PrePrepareMsgPipeline() validation.SignedMessagePipeline {
	return i.fork.PrePrepareMsgPipeline()
}

// PrePrepareMsgPipelineV0 is version 0
func (i *Instance) PrePrepareMsgPipelineV0() validation.SignedMessagePipeline {
	return validation.Combine(
		i.prePrepareMsgValidationPipeline(),
		validation.WrapFunc("add pre-prepare msg", func(signedMessage *message.SignedMessage) error {
			i.Logger.Info("received valid pre-prepare message for round",
				zap.Any("sender_ibft_id", signedMessage.GetSigners()),
				zap.Uint64("round", uint64(signedMessage.Message.Round)))
			i.PrePrepareMessages.AddMessage(signedMessage)
			return nil
		}),
		validation.IfFirstTrueContinueToSecond(
			signed_msg.ValidateRound(i.State().GetRound()),
			i.UponPrePrepareMsg(),
		),
	)
}

func (i *Instance) prePrepareMsgValidationPipeline() validation.SignedMessagePipeline {
	return validation.Combine(
		signed_msg.BasicMsgValidation(),
		signed_msg.MsgTypeCheck(message.ProposalMsgType),
		signed_msg.ValidateLambdas(i.State().GetIdentifier()),
		signed_msg.ValidateSequenceNumber(i.State().GetHeight()),
		signed_msg.AuthorizeMsg(i.ValidatorShare),
		preprepare.ValidatePrePrepareMsg(i.ValueCheck, i.RoundLeader),
	)
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

	if quorum, _, _ := i.changeRoundQuorum(round); quorum {
		notPrepared, highest, err := i.HighestPrepared(round)
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
func (i *Instance) UponPrePrepareMsg() validation.SignedMessagePipeline {
	return validation.WrapFunc("upon pre-prepare msg", func(signedMessage *message.SignedMessage) error {
		// Pre-prepare justification
		err := i.JustifyPrePrepare(uint64(signedMessage.Message.Round), signedMessage.Message.Data)
		if err != nil {
			return errors.Wrap(err, "Unjustified pre-prepare")
		}

		// mark state
		i.ProcessStageChange(proto.RoundState_PrePrepare)

		// broadcast prepare msg
		broadcastMsg := i.generatePrepareMessage(signedMessage.Message.Data)
		if err := i.SignAndBroadcast(broadcastMsg); err != nil {
			i.Logger.Error("could not broadcast prepare message", zap.Error(err))
			return err
		}
		return nil
	})
}

func (i *Instance) generatePrePrepareMessage(value []byte) message.ConsensusMessage {
	return message.ConsensusMessage{
		MsgType:    message.ProposalMsgType,
		Height:     i.State().GetHeight(),
		Round:      i.State().GetRound(),
		Identifier: i.State().GetIdentifier(),
		Data:       value,
	}
}

func (i *Instance) checkExistingPrePrepare(round uint64) (bool, *message.SignedMessage, error) {
	msgs := i.PrePrepareMessages.ReadOnlyMessagesByRound(round)
	if len(msgs) == 1 {
		return true, msgs[0], nil
	} else if len(msgs) > 1 {
		return false, nil, errors.New("multiple pre-preparer msgs, can't decide which one to use")
	}
	return false, nil, nil
}
