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

func (i *Instance) validateCommitMsg() networker.PipelineFunc {
	return func(signedMessage *types.SignedMessage) error {
		// Only 1 prepare per peer per round is valid
		msgs := i.commitMessages.ReadOnlyMessagesByRound(signedMessage.Message.Round)
		if val, found := msgs[signedMessage.IbftId]; found {
			if !val.Message.Compare(*signedMessage.Message) {
				return errors.New(fmt.Sprintf("another (different) commit message for peer %d was received", signedMessage.IbftId))
			}
		}

		// TODO - should we test prepared round as well?

		if err := i.implementation.ValidateCommitMsg(i.state, signedMessage); err != nil {
			return err
		}

		return nil
	}
}

// TODO - passing round can be problematic if the node goes down, it might not know which round it is now.
func (i *Instance) commitQuorum(round uint64, inputValue []byte) (quorum bool, t int, n int) {
	// TODO - do we need to validate round?
	cnt := 0
	msgs := i.commitMessages.ReadOnlyMessagesByRound(round)
	for _, v := range msgs {
		if bytes.Compare(inputValue, v.Message.Value) == 0 {
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
func (i *Instance) uponCommitMsg() networker.PipelineFunc {
	return func(signedMessage *types.SignedMessage) error {
		// add to prepare messages
		i.commitMessages.AddMessage(*signedMessage)
		i.logger.Info("received valid commit message")

		// check if quorum achieved, act upon it.
		quorum, t, n := i.commitQuorum(i.state.PreparedRound, i.state.PreparedValue)
		if i.state.Stage != types.RoundState_Commit && quorum { // if already decided no need to do it again
			i.logger.Info("decided iBFT instance, round %d (%d/%d votes)",
				zap.String("lambda", hex.EncodeToString(i.state.Lambda)),
				zap.Uint64("round", i.state.Round),
				zap.Int("got_votes", t), zap.Int("total_votes", n))

			// mark stage
			i.state.Stage = types.RoundState_Commit

			i.stopRoundChangeTimer()
			i.decided <- true
		}
		return nil
	}
}
