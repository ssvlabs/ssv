package ibft

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"

	"go.uber.org/zap"

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

// TODO - passing round can be problematic if the node goes down, it might not know which round it is now.
func (i *iBFTInstance) commitQuorum(round uint64, inputValue []byte) (quorum bool, t int, n int) {
	// TODO - do we need to validate round?
	cnt := 0
	msgs := i.commitMessages.ReadOnlyMessagesByRound(round)
	for _, v := range msgs {
		if bytes.Compare(inputValue, v.InputValue) == 0 {
			cnt += 1
		}
	}
	quorum = cnt*3 >= i.params.CommitteeSize()*2
	return quorum, cnt, i.params.CommitteeSize()
}

/**
upon receiving a quorum Qcommit of valid ⟨COMMIT, λi, round, value⟩ messages do:
	set timer i to stopped
	Decide(λi , value, Qcommit)
*/
func (i *iBFTInstance) uponCommitMessage(msg *types.Message) {
	if err := i.validateCommit(msg); err != nil {
		i.log.Error("commit message is invalid", zap.Error(err))
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
		i.log.Infof("decided iBFT instance %s, round %d (%d/%d votes)", hex.EncodeToString(i.state.Lambda), i.state.Round, t, n)

		i.state.Stage = types.RoundState_Commit
		i.stopRoundChangeTimer()
		i.decided <- true
	}
}
