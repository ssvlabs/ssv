package ibft

import (
	"errors"

	"github.com/bloxapp/ssv/ibft/types"
)

func (i *iBFTInstance) validatePrePrepare(msg *types.Message) error {
	// Only 1 pre-prepare per round is valid
	if msgs := i.prePrepareMessages.ReadOnlyMessagesByRound(msg.Round); len(msgs) > 0 {
		if !msgs[0].Compare(*msg) {
			return errors.New("another (different) pre-prepare message for the round was received")
		}
	}

	if err := i.implementation.ValidatePrePrepareMsg(i.state, msg); err != nil {
		return err
	}

	return nil
}

func (i *iBFTInstance) uponPrePrepareMessage(msg *types.Message) {
	if err := i.validatePrePrepare(msg); err != nil {
		i.log.WithError(err).Errorf("pre-prepare message is invalid")
	}

	// validate round
	if msg.Round != i.state.Round {
		i.log.Errorf("pre-prepare round %d, expected %d", msg.Round, i.state.Round)
	}

	// add to pre-prepare messages
	i.prePrepareMessages.AddMessage(*msg)
	i.log.Info("received valid pre-prepare message")

	// In case current round is not the first round for the instance, we need to consider previous justifications
	if msg.Round > 0 {
		// TODO
	}

	// broadcast prepare msg
	broadcastMsg := &types.Message{
		Type:       types.MsgType_Prepare,
		Round:      i.state.Round,
		Lambda:     i.state.Lambda,
		InputValue: i.state.InputValue,
		IbftId:     i.state.IBFTId,
	}
	if err := i.network.Broadcast(broadcastMsg); err != nil {
		i.log.WithError(err).Errorf("could not broadcast prepare message")
	}
}
