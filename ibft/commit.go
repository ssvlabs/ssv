package ibft

import (
	"fmt"

	"github.com/bloxapp/ssv/ibft/types"
)

func (i *iBFTInstance) validateCommit(msg *types.Message) error {
	// data
	// leader
	// signature

	if err := i.implementation.ValidateCommitMsg(i.state, msg); err != nil {
		return err
	}

	return nil
}

func (i *iBFTInstance) commitQuorum() bool {
	return false
}

func (i *iBFTInstance) uponCommitMessage(msg *types.Message) error {
	if err := i.validateCommit(msg); err != nil {
		return err
	}

	// validate round
	if msg.Round != i.state.Round {
		return fmt.Errorf("commit round %d, expected %d", msg.Round, i.state.Round)
	}

	// add to prepare messages
	i.commitMessages = append(i.commitMessages, msg)

	// check if quorum achieved, act upon it.
	if i.commitQuorum() {
		// TODO
	}
	return nil
}
