package ibft

import (
	"bytes"
	"github.com/bloxapp/ssv/ibft/pipeline/preprepare"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/pipeline/auth"
	"github.com/bloxapp/ssv/ibft/proto"
)

func (i *Instance) prePrepareMsgPipeline() pipeline.Pipeline {
	return pipeline.Combine(
		i.prePrepareMsgValidationPipeline(),
		pipeline.WrapFunc("add pre-prepare msg", func(signedMessage *proto.SignedMessage) error {
			i.Logger.Info("received valid pre-prepare message for round",
				zap.String("sender_ibft_id", signedMessage.SignersIDString()),
				zap.Uint64("round", signedMessage.Message.Round))
			i.PrePrepareMessages.AddMessage(signedMessage)
			return nil
		}),
		pipeline.IfFirstTrueContinueToSecond(
			auth.ValidateRound(i.State.Round.Get()),
			i.UponPrePrepareMsg(),
		),
	)
}

func (i *Instance) prePrepareMsgValidationPipeline() pipeline.Pipeline {
	return pipeline.Combine(
		auth.BasicMsgValidation(),
		auth.MsgTypeCheck(proto.RoundState_PrePrepare),
		auth.ValidateLambdas(i.State.Lambda.Get()),
		auth.ValidateSequenceNumber(i.State.SeqNumber.Get()),
		auth.AuthorizeMsg(i.ValidatorShare),
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
func (i *Instance) JustifyPrePrepare(round uint64, value []byte) error {
	if round == 1 {
		return nil
	}

	if quorum, _, _ := i.changeRoundQuorum(round); quorum {
		notPrepared, highest, err := i.highestPrepared(round)
		if err != nil {
			return err
		}
		if notPrepared && value == nil {
			return nil
		} else if notPrepared && value != nil {
			return errors.New("unjustified change round for pre-prepare, value should be nil")
		} else if !bytes.Equal(value, highest.PreparedValue) {
			return errors.New("unjustified change round for pre-prepare, value different than highest prepared")
		}
		return nil
	}
	return errors.New("no change round quorum")
}

/*
UponPrePrepareMsg Algorithm 2 IBFT pseudocode for process pi: normal case operation
upon receiving a valid ⟨PRE-PREPARE, λi, ri, value⟩ message m from leader(λi, round) such that:
	JustifyPrePrepare(m) do
		set timer i to running and expire after t(ri)
		broadcast ⟨PREPARE, λi, ri, value⟩
*/
func (i *Instance) UponPrePrepareMsg() pipeline.Pipeline {
	return pipeline.WrapFunc("upon pre-prepare msg", func(signedMessage *proto.SignedMessage) error {
		prepareValueToBroadcast := signedMessage.Message.Value

		// Pre-prepare justification
		err := i.JustifyPrePrepare(signedMessage.Message.Round, prepareValueToBroadcast)
		if err != nil {
			return errors.Wrap(err, "Unjustified pre-prepare")
		}

		// mark State
		i.ProcessStageChange(proto.RoundState_PrePrepare)

		// broadcast prepare msg
		broadcastMsg := i.generatePrepareMessage(prepareValueToBroadcast)
		if err := i.SignAndBroadcast(broadcastMsg); err != nil {
			i.Logger.Error("could not broadcast prepare message", zap.Error(err))
			return err
		}
		return nil
	})
}

func (i *Instance) generatePrePrepareMessage(value []byte) *proto.Message {
	return &proto.Message{
		Type:      proto.RoundState_PrePrepare,
		Round:     i.State.Round.Get(),
		Lambda:    i.State.Lambda.Get(),
		SeqNumber: i.State.SeqNumber.Get(),
		Value:     value,
	}
}
