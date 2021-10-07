package ibft

import (
	"bytes"
	"encoding/hex"
	"github.com/bloxapp/ssv/ibft/pipeline/auth"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/proto"
)

func (i *Instance) prepareMsgPipeline() pipeline.Pipeline {
	return pipeline.Combine(
		auth.BasicMsgValidation(),
		auth.MsgTypeCheck(proto.RoundState_Prepare),
		auth.ValidateLambdas(i.State.Lambda.Get()),
		auth.ValidateSequenceNumber(i.State.SeqNumber.Get()),
		auth.AuthorizeMsg(i.ValidatorShare),
		pipeline.WrapFunc("add prepare msg", func(signedMessage *proto.SignedMessage) error {
			i.Logger.Info("received valid prepare message from round",
				zap.String("sender_ibft_id", signedMessage.SignersIDString()),
				zap.Uint64("round", signedMessage.Message.Round))
			i.PrepareMessages.AddMessage(signedMessage)
			return nil
		}),
		pipeline.IfFirstTrueContinueToSecond(
			auth.ValidateRound((i.State.Round.Get())),
			i.uponPrepareMsg(),
		),
	)
}

// PreparedAggregatedMsg returns a signed message for the state's prepared value with the max known signatures
func (i *Instance) PreparedAggregatedMsg() (*proto.SignedMessage, error) {
	if !i.isPrepared() {
		return nil, errors.New("state not prepared")
	}

	msgs := i.PrepareMessages.ReadOnlyMessagesByRound(i.State.PreparedRound.Get())
	if len(msgs) == 0 {
		return nil, errors.New("no prepare msgs")
	}

	var ret *proto.SignedMessage
	var err error
	for _, msg := range msgs {
		if !bytes.Equal(msg.Message.Value, i.State.PreparedValue.Get()) {
			continue
		}
		if ret == nil {
			ret, err = msg.DeepCopy()
			if err != nil {
				return nil, err
			}
		} else {
			if err := ret.Aggregate(msg); err != nil {
				return nil, err
			}
		}
	}
	return ret, nil
}

/**
### Algorithm 2 IBFT pseudocode for process pi: normal case operation
upon receiving a quorum of valid ⟨PREPARE, λi, ri, value⟩ messages do:
	pri ← ri
	pvi ← value
	broadcast ⟨COMMIT, λi, ri, value⟩
*/
func (i *Instance) uponPrepareMsg() pipeline.Pipeline {
	return pipeline.WrapFunc("upon prepare msg", func(signedMessage *proto.SignedMessage) error {
		// TODO - calculate quorum one way (for prepare, commit, change round and decided) and refactor
		if quorum, _ := i.PrepareMessages.QuorumAchieved(signedMessage.Message.Round, signedMessage.Message.Value); quorum {
			var err error
			i.processPrepareQuorumOnce.Do(func() {
				i.Logger.Info("prepared instance",
					zap.String("Lambda", hex.EncodeToString(i.State.Lambda.Get())), zap.Uint64("round", i.State.Round.Get()))

				// set prepared State
				i.State.PreparedRound.Set(signedMessage.Message.Round)
				i.State.PreparedValue.Set(signedMessage.Message.Value)
				i.ProcessStageChange(proto.RoundState_Prepare)

				// send commit msg
				broadcastMsg := i.generateCommitMessage(i.State.PreparedValue.Get())
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

func (i *Instance) generatePrepareMessage(value []byte) *proto.Message {
	return &proto.Message{
		Type:      proto.RoundState_Prepare,
		Round:     i.State.Round.Get(),
		Lambda:    i.State.Lambda.Get(),
		SeqNumber: i.State.SeqNumber.Get(),
		Value:     value,
	}
}

// isPrepared returns true if instance prepared
func (i *Instance) isPrepared() bool {
	return i.State.PreparedValue.Get() != nil
}
