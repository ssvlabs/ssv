package ibft

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/networker"
	"github.com/bloxapp/ssv/ibft/types"
)

func (i *iBFTInstance) validatePrepareMsg() networker.PipelineFunc {
	return func(signedMessage *types.SignedMessage) error {
		// Only 1 prepare per node per round is valid
		msgs := i.prepareMessages.ReadOnlyMessagesByRound(signedMessage.Message.Round)
		if val, found := msgs[signedMessage.IbftId]; found {
			if !val.Message.Compare(*signedMessage.Message) {
				return errors.New(fmt.Sprintf("another (different) prepare message for peer %d was received", signedMessage.IbftId))
			}
		}

		if err := i.implementation.ValidatePrepareMsg(i.state, signedMessage); err != nil {
			return err
		}

		return nil
	}
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
func (i *iBFTInstance) uponPrepareMsg() networker.PipelineFunc {
	return func(signedMessage *types.SignedMessage) error {
		// TODO - can we process a prepare msg which has different inputValue than the pre-prepare msg?

		// add to prepare messages
		i.prepareMessages.AddMessage(*signedMessage)
		i.logger.Info("received valid prepare message for round", zap.Uint64("round", signedMessage.Message.Round))

		// check if quorum achieved, act upon it.
		if quorum, t, n := i.prepareQuorum(signedMessage.Message.Round, signedMessage.Message.Value); quorum {
			i.logger.Info("prepared instance %s, round %d (%d/%d votes)",
				zap.String("lambda", hex.EncodeToString(i.state.Lambda)), zap.Uint64("round", i.state.Round),
				zap.Int("got_votes", t), zap.Int("total_votes", n))

			// set prepared state
			i.state.PreparedRound = signedMessage.Message.Round
			i.state.PreparedValue = signedMessage.Message.Value
			i.state.Stage = types.RoundState_Prepare

			// send commit msg
			broadcastMsg := &types.Message{
				Type:   types.RoundState_Commit,
				Round:  i.state.Round,
				Lambda: i.state.Lambda,
				Value:  i.state.InputValue,
			}
			if err := i.SignAndBroadcast(broadcastMsg); err != nil {
				i.logger.Error("could not broadcast commit message", zap.Error(err))
				return err
			}
			return nil
		}
		return nil
	}
}
