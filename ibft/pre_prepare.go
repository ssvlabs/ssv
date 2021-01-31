package ibft

import (
	"errors"

	"github.com/bloxapp/ssv/ibft/proto"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/network"
)

func (i *Instance) prePrepareMsgPipeline() network.Pipeline {
	return []network.PipelineFunc{
		MsgTypeCheck(proto.RoundState_PrePrepare),
		i.ValidateLambdas(),
		i.ValidateRound(),
		i.AuthMsg(),
		i.validatePrePrepareMsg(),
		i.uponPrePrepareMsg(),
	}
}

func (i *Instance) validatePrePrepareMsg() network.PipelineFunc {
	return func(signedMessage *proto.SignedMessage) error {
		if signedMessage.IbftId != i.ThisRoundLeader() {
			return errors.New("pre-prepare message sender is not the round's leader")
		}

		if err := i.consensus.ValidateValue(signedMessage.Message.Value); err != nil {
			return err
		}

		return nil
	}
}

func (i *Instance) existingPrePrepareMsg(signedMessage *proto.SignedMessage) bool {
	if msgs := i.prePrepareMessages.ReadOnlyMessagesByRound(signedMessage.Message.Round); len(msgs) > 0 {
		if _, ok := msgs[signedMessage.IbftId]; ok {
			return true
		}
	}
	return false
}

/**
predicate JustifyPrePrepare(hPRE-PREPARE, λi, round, valuei)
	return
		round = 1
		∨ received a quorum Qrc of valid <ROUND-CHANGE, λi, round, prj , pvj> messages such that:
			∀ <ROUND-CHANGE, λi, round, prj , pvj> ∈ Qrc : prj = ⊥ ∧ prj = ⊥
			∨ received a quorum of valid <PREPARE, λi, pr, value> messages such that:
				(pr, value) = HighestPrepared(Qrc)
*/
func (i *Instance) JustifyPrePrepare(round uint64) (bool, error) {
	if round == 1 {
		return true, nil
	}
	quorum, _, _ := i.changeRoundQuorum(round)
	if quorum {
		return i.justifyRoundChange(round)
	}
	return false, nil
}

/**
### Algorithm 2 IBFT pseudocode for process pi: normal case operation
upon receiving a valid ⟨PRE-PREPARE, λi, ri, value⟩ message m from leader(λi, round) such that:
	JustifyPrePrepare(m) do
		set timer i to running and expire after t(ri)
		broadcast ⟨PREPARE, λi, ri, value⟩
*/
func (i *Instance) uponPrePrepareMsg() network.PipelineFunc {
	return func(signedMessage *proto.SignedMessage) error {
		// Only 1 pre-prepare per round is valid
		if i.existingPrePrepareMsg(signedMessage) {
			return nil
		}

		// add to pre-prepare messages
		i.prePrepareMessages.AddMessage(*signedMessage)
		i.Log("received valid pre-prepare message for round",
			false,
			zap.Uint64("sender_ibft_id", signedMessage.IbftId),
			zap.Uint64("round", signedMessage.Message.Round))

		// Pre-prepare justification
		justified, err := i.JustifyPrePrepare(signedMessage.Message.Round)
		if err != nil {
			return err
		}
		if !justified {
			return errors.New("received un-justified pre-prepare message")
		}

		// mark state
		i.state.Stage = proto.RoundState_PrePrepare

		// broadcast prepare msg
		broadcastMsg := &proto.Message{
			Type:   proto.RoundState_Prepare,
			Round:  i.state.Round,
			Lambda: i.state.Lambda,
			Value:  i.state.InputValue,
		}
		if err := i.SignAndBroadcast(broadcastMsg); err != nil {
			i.Log("could not broadcast prepare message", true, zap.Error(err))
			return err
		}
		return nil
	}
}
