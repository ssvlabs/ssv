package ibft

import (
	"fmt"

	"github.com/bloxapp/ssv/ibft/types"
)

func (i *iBFTInstance) validatePrepare(msg *types.Message) error {
	// data
	// leader
	// signature

	if err := i.implementation.ValidatePrepareMsg(i.state, msg); err != nil {
		return err
	}

	return nil
}

func (i *iBFTInstance) prepareQuorum() bool {
	return false
}

func (i *iBFTInstance) uponPrepareMessage(msg *types.Message) error {
	if err := i.validatePrePrepare(msg); err != nil {
		return err
	}

	// validate round
	if msg.Round != i.state.Round {
		return fmt.Errorf("prepare round %d, expected %d", msg.Round, i.state.Round)
	}

	// add to prepare messages
	i.prepareMessages = append(i.prepareMessages, msg)

	// check if quorum achieved, act upon it.
	if i.prepareQuorum() {
		// TODO
	}
	return nil
}
