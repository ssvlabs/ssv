package ibft

import (
	"bytes"

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

func (i *iBFTInstance) prepareQuorum(round uint64, inputValue []byte) bool {
	cnt := uint64(0)
	for _, m := range i.prepareMessages {
		if m.Round == round && bytes.Compare(inputValue, m.InputValue) == 0 {
			cnt += 1
		}
	}
	return cnt*3 >= i.params.IbftCommitteeSize*2
}

func (i *iBFTInstance) uponPrepareMessage(msg *types.Message) {
	if err := i.validatePrePrepare(msg); err != nil {
		i.log.WithError(err).Errorf("prepare message is invalid")
	}

	// validate round
	if msg.Round != i.state.Round {
		i.log.Errorf("prepare round %d, expected %d", msg.Round, i.state.Round)
	}

	// TODO - can we process a prepare msg which has different inputValue than the pre-prepare msg?

	// add to prepare messages
	i.prepareMessages = append(i.prepareMessages, msg)
	i.log.Info("received valid prepare message")

	// check if quorum achieved, act upon it.
	if i.prepareQuorum(msg.Round, msg.InputValue) {
		// set prepared state
		i.state.PreparedRound = msg.Round
		i.state.PreparedValue = msg.InputValue

		if err := i.network.Broadcast(i.implementation.NewCommitMsg(i.state)); err != nil {
			i.log.WithError(err).Errorf("could not broadcast commit message")
		}
	}
}
