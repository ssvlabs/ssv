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

func (i *Instance) PrePrepareMsgPipeline() pipeline.Pipeline {
	return i.fork.PrePrepareMsgPipeline()
}

// PrePrepareMsgPipelineV0
func (i *Instance) PrePrepareMsgPipelineV0() pipeline.Pipeline {
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
			auth.ValidateRound(i.State().Round.Get()),
			i.UponPrePrepareMsg(),
		),
	)
}

func (i *Instance) prePrepareMsgValidationPipeline() pipeline.Pipeline {
	return pipeline.Combine(
		auth.BasicMsgValidation(),
		auth.MsgTypeCheck(proto.RoundState_PrePrepare),
		auth.ValidateLambdas(i.State().Lambda.Get()),
		auth.ValidateSequenceNumber(i.State().SeqNumber.Get()),
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
func (i *Instance) UponPrePrepareMsg() pipeline.Pipeline {
	return pipeline.WrapFunc("upon pre-prepare msg", func(signedMessage *proto.SignedMessage) error {
		// Pre-prepare justification
		err := i.JustifyPrePrepare(signedMessage.Message.Round, signedMessage.Message.Value)
		if err != nil {
			return errors.Wrap(err, "Unjustified pre-prepare")
		}

		// mark state
		i.ProcessStageChange(proto.RoundState_PrePrepare)

		// broadcast prepare msg
		broadcastMsg := i.generatePrepareMessage(signedMessage.Message.Value)
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
		Round:     i.State().Round.Get(),
		Lambda:    i.State().Lambda.Get(),
		SeqNumber: i.State().SeqNumber.Get(),
		Value:     value,
	}
}

func (i *Instance) checkExistingPrePrepare(round uint64) (bool, *proto.SignedMessage, error) {
	msgs := i.PrePrepareMessages.ReadOnlyMessagesByRound(round)
	if len(msgs) == 1 {
		return true, msgs[0], nil
	} else if len(msgs) > 1 {
		return false, nil, errors.New("multiple pre-preparer msgs, can't decide which one to use")
	}
	return false, nil, nil
}
