package ibft

import (
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/pipeline/auth"
	"github.com/bloxapp/ssv/ibft/pipeline/preprepare"
	"github.com/bloxapp/ssv/ibft/proto"
)

func (i *Instance) prePrepareMsgPipeline() pipeline.Pipeline {
	return pipeline.Combine(
		auth.MsgTypeCheck(proto.RoundState_PrePrepare),
		auth.ValidateLambdas(i.State.Lambda),
		auth.ValidateRound(i.State.Round),
		auth.ValidatePKs(i.State.ValidatorPk),
		auth.ValidateSequenceNumber(i.State.SeqNumber),
		auth.AuthorizeMsg(i.Params),
		preprepare.ValidatePrePrepareMsg(i.ValueCheck, i.LeaderSelector, i.Params),
		i.UponPrePrepareMsg(),
	)
}

// JustifyPrePrepare implements:
// predicate JustifyPrePrepare(hPRE-PREPARE, λi, round, valuei)
// 	return
// 		round = 1
// 		∨ received a quorum Qrc of valid <ROUND-CHANGE, λi, round, prj , pvj> messages such that:
// 			∀ <ROUND-CHANGE, λi, round, prj , pvj> ∈ Qrc : prj = ⊥ ∧ prj = ⊥
// 			∨ received a quorum of valid <PREPARE, λi, pr, value> messages such that:
// 				(pr, value) = HighestPrepared(Qrc)
func (i *Instance) JustifyPrePrepare(round uint64) (bool, error) {
	if round == 1 {
		return true, nil
	}

	if quorum, _, _ := i.changeRoundQuorum(round); quorum {
		return i.JustifyRoundChange(round)
	}
	return false, nil
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
		// add to pre-prepare messages
		i.PrePrepareMessages.AddMessage(signedMessage)
		i.Logger.Info("received valid pre-prepare message for round",
			zap.String("sender_ibft_id", signedMessage.SignersIDString()),
			zap.Uint64("round", signedMessage.Message.Round))

		// Pre-prepare justification
		justified, err := i.JustifyPrePrepare(signedMessage.Message.Round)
		if err != nil {
			return err
		}
		if !justified {
			return errors.New("received un-justified pre-prepare message")
		}

		// mark State
		i.SetStage(proto.RoundState_PrePrepare)

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
		Type:        proto.RoundState_PrePrepare,
		Round:       i.State.Round,
		Lambda:      i.State.Lambda,
		SeqNumber:   i.State.SeqNumber,
		Value:       value,
		ValidatorPk: i.State.ValidatorPk,
	}
}
