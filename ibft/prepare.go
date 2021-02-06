package ibft

import (
	"bytes"
	"encoding/hex"
	"errors"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/proto"
)

func (i *Instance) prepareMsgPipeline() Pipeline {
	return []PipelineFunc{
		MsgTypeCheck(proto.RoundState_Prepare),
		i.ValidateLambdas(),
		i.ValidateRound(),
		i.AuthMsg(),
		i.validatePrepareMsg(),
		i.uponPrepareMsg(),
	}
}

func (i *Instance) validatePrepareMsg() PipelineFunc {
	return func(signedMessage *proto.SignedMessage) error {
		// Validate we received a pre-prepare msg for this round and
		// that it's value is equal to the prepare msg
		val, err := i.PrePrepareValue(signedMessage.Message.Round)
		if err != nil {
			return err // will return error if no valid pre-prepare value was received
		}

		if !bytes.Equal(val, signedMessage.Message.Value) {
			return errors.New("pre-prepare value not equal to prepare msg value")
		}

		return nil
	}
}

func (i *Instance) batchedPrepareMsgs(round uint64) map[string][]*proto.SignedMessage {
	msgs := i.prepareMessages.ReadOnlyMessagesByRound(round)
	ret := make(map[string][]*proto.SignedMessage)
	for _, msg := range msgs {
		valueHex := hex.EncodeToString(msg.Message.Value)
		if ret[valueHex] == nil {
			ret[valueHex] = make([]*proto.SignedMessage, 0)
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

func (i *Instance) existingPrepareMsg(signedMessage *proto.SignedMessage) bool {
	// TODO - not sure the spec requires unique votes.
	msgs := i.prepareMessages.ReadOnlyMessagesByRound(signedMessage.Message.Round)
	for _, id := range signedMessage.SignerIds {
		if _, found := msgs[id]; found {
			return true
		}
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
func (i *Instance) uponPrepareMsg() PipelineFunc {
	// TODO - concurrency lock?
	return func(signedMessage *proto.SignedMessage) error {
		// TODO - can we process a prepare msg which has different inputValue than the pre-prepare msg?
		// Only 1 prepare per node per round is valid
		if i.existingPrepareMsg(signedMessage) {
			return nil
		}

		// add to prepare messages
		i.prepareMessages.AddMessage(signedMessage)
		i.logger.Info("received valid prepare message from round",
			zap.String("sender_ibft_id", signedMessage.SignersIdString()),
			zap.Uint64("round", signedMessage.Message.Round))

		// check if quorum achieved, act upon it.
		if i.State.Stage == proto.RoundState_Prepare {
			return nil // no reason to prepare again
		}
		if quorum, t, n := i.prepareQuorum(signedMessage.Message.Round, signedMessage.Message.Value); quorum {
			i.logger.Info("prepared instance",
				zap.String("Lambda", hex.EncodeToString(i.State.Lambda)), zap.Uint64("round", i.State.Round),
				zap.Int("got_votes", t), zap.Int("total_votes", n))

			// set prepared State
			i.State.PreparedRound = signedMessage.Message.Round
			i.State.PreparedValue = signedMessage.Message.Value
			i.SetStage(proto.RoundState_Prepare)

			// send commit msg
			broadcastMsg := &proto.Message{
				Type:           proto.RoundState_Commit,
				Round:          i.State.Round,
				Lambda:         i.State.Lambda,
				PreviousLambda: i.State.PreviousLambda,
				Value:          i.State.InputValue,
			}
			if err := i.SignAndBroadcast(broadcastMsg); err != nil {
				i.logger.Info("could not broadcast commit message", zap.Error(err))
				return err
			}
			return nil
		}
		return nil
	}
}
