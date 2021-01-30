package ibft

import (
	"errors"

	"github.com/bloxapp/ssv/ibft/proto"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/network"
)

func (i *Instance) validatePrePrepareMsg() network.PipelineFunc {
	return func(signedMessage *proto.SignedMessage) error {
		// TODO - validate proposer correct
		if signedMessage.IbftId != i.RoundLeader() {
			return errors.New("pre-prepare message sent not by leader")
		}

		if err := i.consensus.ValidateValue(signedMessage.Message.Value); err != nil {
			return err
		}

		return nil
	}
}

func (i *Instance) existingPreprepareMsg(signedMessage *proto.SignedMessage) bool {
	if msgs := i.prePrepareMessages.ReadOnlyMessagesByRound(signedMessage.Message.Round); len(msgs) > 0 {
		if _, ok := msgs[signedMessage.IbftId]; ok {
			return true
		}
	}
	return false
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
		if i.existingPreprepareMsg(signedMessage) {
			return nil
		}

		// add to pre-prepare messages
		i.prePrepareMessages.AddMessage(*signedMessage)
		i.logger.Info("received valid pre-prepare message for round", zap.Uint64("sender_ibft_id", signedMessage.IbftId), zap.Uint64("round", signedMessage.Message.Round))

		// In case current round is not the first round for the instance, we need to consider previous justifications
		if signedMessage.Message.Round > 0 {
			// TODO
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
			i.logger.Error("could not broadcast prepare message", zap.Error(err))
			return err
		}
		return nil
	}
}
