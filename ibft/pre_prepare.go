package ibft

import (
	"fmt"

	"github.com/bloxapp/ssv/ibft/types"
)

func (i *iBFTInstance) validatePrePrepare(msg *types.Message) error {
	// data
	// leader
	// signature

	if err := i.implementation.ValidatePrePrepareMsg(i.state, msg); err != nil {
		return err
	}

	return nil
}

func (i *iBFTInstance) uponPrePrepareMessage(msg *types.Message) error {
	if err := i.validatePrePrepare(msg); err != nil {
		return err
	}

	// validate round
	if msg.Round != i.state.Round {
		return fmt.Errorf("pre-prepare round %d, expected %d", msg.Round, i.state.Round)
	}

	// add to pre-prepare messages
	i.prePrepareMessages = append(i.prePrepareMessages, msg)

	// In case current round is not the first round for the instance, we need to consider previous justifications
	if msg.Round > 0 {
		// TODO
	}

	if err := i.network.Broadcast(i.implementation.NewPrepareMsg(i.state)); err != nil {
		return err
	}
	return nil
}
