package instance

import (
	"bytes"
	"encoding/hex"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"

	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/signedmsg"

	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// PrepareMsgPipeline is the main prepare msg pipeline
func (i *Instance) PrepareMsgPipeline() pipelines.SignedMessagePipeline {
	return i.fork.PrepareMsgPipeline()
}

// PrepareMsgPipelineV0 is the v0 version
func (i *Instance) PrepareMsgPipelineV0() pipelines.SignedMessagePipeline {
	return pipelines.Combine(
		signedmsg.BasicMsgValidation(),
		signedmsg.MsgTypeCheck(message.PrepareMsgType),
		signedmsg.ValidateLambdas(i.State().GetIdentifier()),
		signedmsg.ValidateSequenceNumber(i.State().GetHeight()),
		signedmsg.AuthorizeMsg(i.ValidatorShare),
		pipelines.WrapFunc("add prepare msg", func(signedMessage *message.SignedMessage) error {
			i.Logger.Info("received valid prepare message from round",
				zap.Any("sender_ibft_id", signedMessage.GetSigners()),
				zap.Uint64("round", uint64(signedMessage.Message.Round)))
			i.PrepareMessages.AddMessage(signedMessage)
			return nil
		}),
		pipelines.CombineQuiet(
			signedmsg.ValidateRound(i.State().GetRound()),
			i.uponPrepareMsg(),
		),
	)
}

// PreparedAggregatedMsg returns a signed message for the state's prepared value with the max known signatures
func (i *Instance) PreparedAggregatedMsg() (*message.SignedMessage, error) {
	if !i.isPrepared() {
		return nil, errors.New("state not prepared")
	}

	msgs := i.PrepareMessages.ReadOnlyMessagesByRound(i.State().GetPreparedRound())
	if len(msgs) == 0 {
		return nil, errors.New("no prepare msgs")
	}

	var ret *message.SignedMessage
	for _, msg := range msgs {
		if !bytes.Equal(msg.Message.Data, i.State().GetPreparedValue()) {
			continue
		}
		if ret == nil {
			ret = msg.DeepCopy()
		} else {
			if err := ret.Aggregate(msg); err != nil {
				return nil, err
			}
		}
	}
	return ret, nil
}

/**
### Algorithm 2 IBFTController pseudocode for process pi: normal case operation
upon receiving a quorum of valid ⟨PREPARE, λi, ri, value⟩ messages do:
	pri ← ri
	pvi ← value
	broadcast ⟨COMMIT, λi, ri, value⟩
*/
func (i *Instance) uponPrepareMsg() pipelines.SignedMessagePipeline {
	return pipelines.WrapFunc("upon prepare msg", func(signedMessage *message.SignedMessage) error {
		// TODO - calculate quorum one way (for prepare, commit, change round and decided) and refactor
		if quorum, _ := i.PrepareMessages.QuorumAchieved(signedMessage.Message.Round, signedMessage.Message.Data); quorum {
			var err error
			i.processPrepareQuorumOnce.Do(func() {
				i.Logger.Info("prepared instance",
					zap.String("Lambda", hex.EncodeToString(i.State().GetIdentifier())), zap.Any("round", i.State().GetRound()))

				// set prepared state
				i.State().PreparedRound.Store(signedMessage.Message.Round)
				i.State().PreparedValue.Store(signedMessage.Message.Data)
				i.ProcessStageChange(qbft.RoundState_Prepare)

				// send commit msg
				broadcastMsg := i.generateCommitMessage(i.State().GetPreparedValue())
				if e := i.SignAndBroadcast(broadcastMsg); e != nil {
					i.Logger.Info("could not broadcast commit message", zap.Error(err))
					err = e
				}
			})
			return err
		}
		return nil
	})
}

func (i *Instance) generatePrepareMessage(value []byte) *message.ConsensusMessage {
	return &message.ConsensusMessage{
		MsgType:    message.PrepareMsgType,
		Height:     i.State().GetHeight(),
		Round:      i.State().GetRound(),
		Identifier: i.State().GetIdentifier(),
		Data:       value,
	}
}

// isPrepared returns true if instance prepared
func (i *Instance) isPrepared() bool {
	return i.State().GetPreparedValue() != nil
}
