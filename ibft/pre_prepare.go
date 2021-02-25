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
		i.WaitForStage(),
		auth.MsgTypeCheck(proto.RoundState_PrePrepare),
		auth.ValidateLambdas(i.State),
		auth.ValidateRound(i.State),
		auth.AuthorizeMsg(i.params),
		preprepare.ValidatePrePrepareMsg(i.consensus, i.State, i.params),
		i.uponPrePrepareMsg(),
	)
}

// JustifyPrePrepare -- TODO
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
		return i.justifyRoundChange(round)
	}

	return false, nil
}

// PrePrepareValue checks round and returns message value
func (i *Instance) PrePrepareValue(round uint64) ([]byte, error) {
	msgs := i.prePrepareMessages.ReadOnlyMessagesByRound(round)
	if msg, found := msgs[i.RoundLeader(round)]; found {
		return msg.Message.Value, nil
	}
	return nil, errors.Errorf("no pre-prepare value found for round %d", round)
}

func (i *Instance) existingPrePrepareMsg(signedMessage *proto.SignedMessage) bool {
	val, _ := i.PrePrepareValue(signedMessage.Message.Round)
	return len(val) > 0
}

/**
### Algorithm 2 IBFT pseudocode for process pi: normal case operation
upon receiving a valid ⟨PRE-PREPARE, λi, ri, value⟩ message m from leader(λi, round) such that:
	JustifyPrePrepare(m) do
		set timer i to running and expire after t(ri)
		broadcast ⟨PREPARE, λi, ri, value⟩
*/
func (i *Instance) uponPrePrepareMsg() pipeline.Pipeline {
	return pipeline.WrapFunc(func(signedMessage *proto.SignedMessage) error {
		// Only 1 pre-prepare per round is valid
		if i.existingPrePrepareMsg(signedMessage) {
			return nil
		}

		// add to pre-prepare messages
		i.prePrepareMessages.AddMessage(signedMessage)
		i.logger.Info("received valid pre-prepare message for round",
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
		broadcastMsg := &proto.Message{
			Type:           proto.RoundState_Prepare,
			Round:          i.State.Round,
			Lambda:         i.State.Lambda,
			PreviousLambda: i.State.PreviousLambda,
			Value:          i.State.InputValue,
		}
		if err := i.SignAndBroadcast(broadcastMsg); err != nil {
			i.logger.Error("could not broadcast prepare message", zap.Error(err))
			return err
		}
		return nil
	})
}
