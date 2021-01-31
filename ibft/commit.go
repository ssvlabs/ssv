package ibft

import (
	"bytes"
	"encoding/hex"

	"github.com/bloxapp/ssv/ibft/proto"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/network"
)

func (i *Instance) commitMsgPipeline() network.Pipeline {
	return []network.PipelineFunc{
		MsgTypeCheck(proto.RoundState_Commit),
		i.ValidateLambdas(),
		i.ValidateRound(),
		i.AuthMsg(),
		i.validateCommitMsg(),
		i.uponCommitMsg(),
	}
}

func (i *Instance) validateCommitMsg() network.PipelineFunc {
	return func(signedMessage *proto.SignedMessage) error {
		// TODO - should we test prepared round as well?

		if err := i.consensus.ValidateValue(signedMessage.Message.Value); err != nil {
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
		if bytes.Equal(inputValue, v.Message.Value) {
			cnt += 1
		}
	}
	quorum = cnt*3 >= i.params.CommitteeSize()*2
	return quorum, cnt, i.params.CommitteeSize()
}

func (i *Instance) existingCommitMsg(signedMessage *proto.SignedMessage) bool {
	msgs := i.commitMessages.ReadOnlyMessagesByRound(signedMessage.Message.Round)
	if _, found := msgs[signedMessage.IbftId]; found {
		return true
	}
	return false
}

/**
upon receiving a quorum Qcommit of valid ⟨COMMIT, λi, round, value⟩ messages do:
	set timer i to stopped
	Decide(λi , value, Qcommit)
*/
func (i *Instance) uponCommitMsg() network.PipelineFunc {
	// TODO - concurrency lock?
	return func(signedMessage *proto.SignedMessage) error {
		// Only 1 prepare per peer per round is valid
		if i.existingCommitMsg(signedMessage) {
			return nil
		}

		// add to prepare messages
		i.commitMessages.AddMessage(*signedMessage)
		i.Log("received valid commit message for round", false, zap.Uint64("sender_ibft_id", signedMessage.IbftId), zap.Uint64("round", signedMessage.Message.Round))

		// check if quorum achieved, act upon it.
		if i.state.Stage == proto.RoundState_Decided {
			return nil // no reason to commit again
		}
		quorum, t, n := i.commitQuorum(i.state.PreparedRound, i.state.PreparedValue)
		if quorum { // if already decided no need to do it again
			i.Log("decided iBFT instance",
				false,
				zap.String("lambda", hex.EncodeToString(i.state.Lambda)), zap.Uint64("round", i.state.Round),
				zap.Int("got_votes", t), zap.Int("total_votes", n))

			// mark stage
			i.state.Stage = proto.RoundState_Decided

			i.stopRoundChangeTimer()
			i.decided <- true
		}
		return nil
	}
}
