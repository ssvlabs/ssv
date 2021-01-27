package ibft

import (
	"encoding/hex"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/types"
	"github.com/bloxapp/ssv/networker"
)

func (i *Instance) validatePrepareMsg() networker.PipelineFunc {
	return func(signedMessage *types.SignedMessage) error {
		// TODO - prepare should equal pre-prepare value

		if err := i.implementation.ValidatePrepareMsg(i.state, signedMessage); err != nil {
			return err
		}

		return nil
	}
}

func (i *Instance) batchedPrepareMsgs(round uint64) map[string][]types.SignedMessage {
	msgs := i.prepareMessages.ReadOnlyMessagesByRound(round)
	ret := make(map[string][]types.SignedMessage)
	for _, msg := range msgs {
		valueHex := hex.EncodeToString(msg.Message.Value)
		if ret[valueHex] == nil {
			ret[valueHex] = make([]types.SignedMessage, 0)
		}
		ret[valueHex] = append(ret[valueHex], msg)
	}
	return ret
}

// TODO - passing round can be problematic if the node goes down, it might not know which round it is now.
func (i *Instance) prepareQuorum(round uint64, inputValue []byte) (quorum bool, t int, n int) {
	batched := i.batchedPrepareMsgs(round)
	if msgs, ok := batched[hex.EncodeToString(inputValue)]; ok {
		quorum = len(msgs)*3 >= i.params.CommitteeSize()*2
		return quorum, len(msgs), i.params.CommitteeSize()
	}

	return false, 0, i.params.CommitteeSize()
}

func (i *Instance) existingPrepareMsg(signedMessage *types.SignedMessage) bool {
	msgs := i.prepareMessages.ReadOnlyMessagesByRound(signedMessage.Message.Round)
	if _, found := msgs[signedMessage.IbftId]; found {
		return true
	}
	return false
}

/**
### Algorithm 2 IBFT pseudocode for process pi: normal case operation
upon receiving a quorum of valid ⟨PREPARE, λi, ri, value⟩ messages do:
	pri ← ri
	pvi ← value
	broadcast ⟨COMMIT, λi, ri, value⟩
*/
func (i *Instance) uponPrepareMsg() networker.PipelineFunc {
	// TODO - concurrency lock?
	return func(signedMessage *types.SignedMessage) error {
		// TODO - can we process a prepare msg which has different inputValue than the pre-prepare msg?
		// Only 1 prepare per node per round is valid
		if i.existingPrepareMsg(signedMessage) {
			return nil
		}

		// add to prepare messages
		i.prepareMessages.AddMessage(*signedMessage)
		i.logger.Info("received valid prepare message from round", zap.Uint64("sender_ibft_id", signedMessage.IbftId), zap.Uint64("round", signedMessage.Message.Round))

		// check if quorum achieved, act upon it.
		if i.state.Stage == types.RoundState_Prepare {
			return nil // no reason to prepare again
		}
		if quorum, t, n := i.prepareQuorum(signedMessage.Message.Round, signedMessage.Message.Value); quorum {
			i.logger.Info("prepared instance",
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
