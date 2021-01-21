package ibft

import (
	"bytes"
	"encoding/hex"
	"errors"

	"github.com/bloxapp/ssv/ibft/types"
)

func (i *iBFTInstance) validateCommit(msg *types.Message) error {
	if !bytes.Equal(i.state.PreparedValue, msg.InputValue) {
		return errors.New("prepared input value different than commit message input value")
	}

	// TODO - should we test prepared round as well?

	if err := i.implementation.ValidateCommitMsg(i.state, msg); err != nil {
		return err
	}

	return nil
}

func (i *iBFTInstance) commitQuorum(inputValue []byte) bool {
	// TODO - do we need to test round?
	cnt := uint64(0)
	for _, m := range i.prepareMessages {
		if bytes.Compare(inputValue, m.InputValue) == 0 {
			cnt += 1
		}
	}
	return cnt*3 >= i.params.IbftCommitteeSize*2
}

func (i *iBFTInstance) uponCommitMessage(msg *types.Message) {
	if err := i.validateCommit(msg); err != nil {
		i.log.WithError(err).Errorf("commit message is invalid")
	}

	// validate round
	// TODO - should we test round?
	//if msg.Round != i.state.Round {
	//	i.log.Errorf("commit round %d, expected %d", msg.Round, i.state.Round)
	//	return fmt.Errorf("commit round %d, expected %d", msg.Round, i.state.Round)
	//}

	// add to prepare messages
	i.commitMessages = append(i.commitMessages, msg)
	i.log.Info("received valid commit message")

	// check if quorum achieved, act upon it.
	if i.commitQuorum(i.state.InputValue) {
		i.log.Infof("concluded iBFT instance %s", hex.EncodeToString(i.state.Lambda))
		i.committed <- true
	}
}
