package ibft

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/bloxapp/ssv/ibft/types"
)

func (i *iBFTInstance) validateCommit(msg *types.Message) error {
	// Only 1 prepare per peer per round is valid
	msgs := i.commitMessages.ReadOnlyMessagesByRound(msg.Round)
	if val, found := msgs[msg.IbftId]; found {
		if !val.Compare(*msg) {
			return errors.New(fmt.Sprintf("another (different) commit message for peer %d was received", msg.IbftId))
		}
	}

	// TODO - should we test prepared round as well?

	if err := i.implementation.ValidateCommitMsg(i.state, msg); err != nil {
		return err
	}

	return nil
}

func (i *iBFTInstance) commitQuorum(round uint64, inputValue []byte) (quorum bool, t uint64, n uint64) {
	// TODO - do we need to test round?
	cnt := uint64(0)
	msgs := i.commitMessages.ReadOnlyMessagesByRound(round)
	for _, v := range msgs {
		if bytes.Compare(inputValue, v.InputValue) == 0 {
			cnt += 1
		}
	}
	quorum = cnt*3 >= i.params.IbftCommitteeSize*2
	return quorum, cnt, i.params.IbftCommitteeSize
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
	i.commitMessages.AddMessage(*msg)
	i.log.Info("received valid commit message")

	// check if quorum achieved, act upon it.
	if quorum, t, n := i.commitQuorum(i.state.PreparedRound, i.state.PreparedValue); quorum {
		i.log.Infof("concluded iBFT instance %s (%d out of %d)", hex.EncodeToString(i.state.Lambda), t, n)
		i.committed <- true
	}
}
