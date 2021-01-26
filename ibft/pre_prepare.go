package ibft

import (
	"errors"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/networker"
	"github.com/bloxapp/ssv/ibft/types"
)

func (i *iBFTInstance) validatePrePrepareMsg() networker.PipelineFunc {
	return func(signedMessage *types.SignedMessage) error {
		// TODO - validate proposer correct

		// Only 1 pre-prepare per round is valid
		if msgs := i.prePrepareMessages.ReadOnlyMessagesByRound(signedMessage.Message.Round); len(msgs) > 0 {
			if !msgs[0].Message.Compare(*signedMessage.Message) {
				return errors.New("another (different) pre-prepare message for the round was received")
			}
		}

		if err := i.implementation.ValidatePrePrepareMsg(i.state, signedMessage); err != nil {
			return err
		}

		return nil
	}
}

/**
### Algorithm 2 IBFT pseudocode for process pi: normal case operation
upon receiving a valid ⟨PRE-PREPARE, λi, ri, value⟩ message m from leader(λi, round) such that:
	JustifyPrePrepare(m) do
		set timer i to running and expire after t(ri)
		broadcast ⟨PREPARE, λi, ri, value⟩
*/
func (i *iBFTInstance) uponPrePrepareMsg() networker.PipelineFunc {
	return func(signedMessage *types.SignedMessage) error {
		// add to pre-prepare messages
		i.prePrepareMessages.AddMessage(*signedMessage)
		i.logger.Info("received valid pre-prepare message")

		// In case current round is not the first round for the instance, we need to consider previous justifications
		if signedMessage.Message.Round > 0 {
			// TODO
		}

		// mark state
		i.state.Stage = types.RoundState_PrePrepare

		// broadcast prepare msg
		broadcastMsg := &types.Message{
			Type:   types.RoundState_Prepare,
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
