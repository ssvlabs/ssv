package ibft

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/bloxapp/ssv/ibft/types"
	"go.uber.org/zap"
)

func (i *iBFTInstance) validatePrepare(msg *types.SignedMessage) error {
	// Only 1 prepare per peer per round is valid
	msgs := i.prepareMessages.ReadOnlyMessagesByRound(msg.Message.Round)
	if val, found := msgs[msg.IbftId]; found {
		if !val.Message.Compare(*msg.Message) {
			return errors.New(fmt.Sprintf("another (different) prepare message for peer %d was received", msg.IbftId))
		}
	}

	if err := i.implementation.ValidatePrepareMsg(i.state, msg); err != nil {
		return err
	}

	return nil
}

// TODO - passing round can be problematic if the node goes down, it might not know which round it is now.
func (i *iBFTInstance) prepareQuorum(round uint64, inputValue []byte) (quorum bool, t int, n int) {
	cnt := 0
	msgs := i.prepareMessages.ReadOnlyMessagesByRound(round)
	for _, v := range msgs {
		if bytes.Compare(inputValue, v.Message.Value) == 0 {
			cnt += 1
		}
	}

	quorum = cnt*3 >= i.params.CommitteeSize()*2
	return quorum, cnt, i.params.CommitteeSize()
}

/**
### Algorithm 2 IBFT pseudocode for process pi: normal case operation
upon receiving a quorum of valid ⟨PREPARE, λi, ri, value⟩ messages do:
	pri ← ri
	pvi ← value
	broadcast ⟨COMMIT, λi, ri, value⟩
*/
func (i *iBFTInstance) uponPrepareMessage(msg *types.SignedMessage) {
	if err := i.validatePrepare(msg); err != nil {
		i.log.Error("prepare message is invalid", zap.Error(err))
	}

	// validate round
	if msg.Message.Round != i.state.Round {
		i.log.Error("got unexpected prepare round", zap.Uint64("expected_round", msg.Message.Round), zap.Uint64("got_round", i.state.Round))
	}

	// TODO - can we process a prepare msg which has different inputValue than the pre-prepare msg?

	// add to prepare messages
	i.prepareMessages.AddMessage(*msg)
	i.log.Info("received valid prepare message for round", zap.Uint64("round", msg.Message.Round))

	// check if quorum achieved, act upon it.
	if quorum, t, n := i.prepareQuorum(msg.Message.Round, msg.Message.Value); quorum {
		i.log.Infof("prepared instance %s, round %d (%d/%d votes)", hex.EncodeToString(i.state.Lambda), i.state.Round, t, n)

		// set prepared state
		i.state.PreparedRound = msg.Message.Round
		i.state.PreparedValue = msg.Message.Value
		i.state.Stage = types.RoundState_Prepare

		// send commit msg
		broadcastMsg := &types.Message{
			Type:   types.RoundState_Commit,
			Round:  i.state.Round,
			Lambda: i.state.Lambda,
			Value:  i.state.InputValue,
		}
		if err := i.network.Broadcast(broadcastMsg); err != nil {
			i.log.Error("could not broadcast commit message", zap.Error(err))
		}
	}
}
