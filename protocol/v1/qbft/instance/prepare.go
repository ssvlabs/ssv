package instance

import (
	"bytes"
	"encoding/hex"
	"fmt"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/signedmsg"
)

// PrepareMsgPipeline is the main prepare msg pipeline
func (i *Instance) PrepareMsgPipeline() pipelines.SignedMessagePipeline {
	validationPipeline := i.PrepareMsgValidationPipeline()

	return pipelines.Combine(
		signedmsg.ProposalExists(i.State()),
		pipelines.WrapFunc(validationPipeline.Name(), func(signedMessage *specqbft.SignedMessage) error {
			if err := validationPipeline.Run(signedMessage); err != nil {
				return fmt.Errorf("invalid prepare message: %w", err)
			}
			return nil
		}),

		i.uponPrepareMsg(),
	)
}

// PrepareMsgValidationPipeline is the prepare msg validation pipeline.
func (i *Instance) PrepareMsgValidationPipeline() pipelines.SignedMessagePipeline {
	return pipelines.Combine(
		i.fork.PrepareMsgValidationPipeline(i.ValidatorShare, i.State()),
		pipelines.WrapFunc("add prepare msg", func(signedMessage *specqbft.SignedMessage) error {
			i.Logger.Info("received valid prepare message from round",
				zap.Any("sender_ibft_id", signedMessage.GetSigners()),
				zap.Uint64("round", uint64(signedMessage.Message.Round)))

			prepareMsg, err := signedMessage.Message.GetPrepareData()
			if err != nil {
				return fmt.Errorf("could not get prepare data: %w", err)
			}
			i.containersMap[specqbft.PrepareMsgType].AddMessage(signedMessage, prepareMsg.Data)
			return nil
		}),
	)
}

// PreparedAggregatedMsg returns a signed message for the state's prepared value with the max known signatures
func (i *Instance) PreparedAggregatedMsg() (*specqbft.SignedMessage, error) {
	if !i.isPrepared() {
		return nil, errors.New("state not prepared")
	}

	msgs := i.containersMap[specqbft.PrepareMsgType].ReadOnlyMessagesByRound(i.State().GetPreparedRound())
	if len(msgs) == 0 {
		return nil, errors.New("no prepare msgs")
	}

	var ret *specqbft.SignedMessage
	for _, msg := range msgs {
		p, err := msg.Message.GetPrepareData()
		if err != nil {
			i.Logger.Warn("failed to get prepare data", zap.Error(err))
			continue
		}
		if !bytes.Equal(p.Data, i.State().GetPreparedValue()) {
			i.Logger.Warn("prepared value is not equal to prepare data", zap.String("stateValue", hex.EncodeToString(i.State().GetPreparedValue())), zap.String("prepareData", hex.EncodeToString(p.Data)))
			continue
		}
		if ret == nil {
			ret = msg.DeepCopy()
		} else {
			if err := message.Aggregate(ret, msg); err != nil {
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
	return pipelines.WrapFunc("upon prepare msg", func(signedMessage *specqbft.SignedMessage) error {

		prepareData, err := signedMessage.Message.GetPrepareData()
		if err != nil {
			return err
		}

		if quorum, _, _ := signedmsg.HasQuorum(i.ValidatorShare, i.containersMap[specqbft.PrepareMsgType].ReadOnlyMessagesByRound(i.State().GetRound())); !quorum {
			return nil
		}

		if i.didSendCommitForHeightAndRound() {
			return nil // already moved to commit stage
		}

		var errorPrp error
		i.processPrepareQuorumOnce.Do(func() {
			i.Logger.Info("prepared instance",
				zap.String("Lambda", hex.EncodeToString(i.State().GetIdentifier())), zap.Any("round", i.State().GetRound()))

			// set prepared state
			i.State().PreparedValue.Store(prepareData.Data) // passing the data as is, and not get the specqbft.PrepareData cause of msgCount saves that way
			i.State().PreparedRound.Store(i.State().GetRound())
			i.ProcessStageChange(qbft.RoundStatePrepare)

			// send commit msg
			broadcastMsg, err := i.generateCommitMessage(i.State().GetPreparedValue())
			if err != nil {
				return
			}
			if e := i.SignAndBroadcast(broadcastMsg); e != nil {
				i.Logger.Info("could not broadcast commit message", zap.Error(err))
				errorPrp = e
			}
		})
		return errorPrp

	})
}

func (i *Instance) didSendCommitForHeightAndRound() bool {
	for _, msg := range i.containersMap[specqbft.CommitMsgType].ReadOnlyMessagesByRound(i.State().GetRound()) {
		if msg.MatchedSigners([]types.OperatorID{i.ValidatorShare.NodeID}) {
			return true
		}
	}
	return false
}

func (i *Instance) generatePrepareMessage(value []byte) (*specqbft.Message, error) {
	prepareMsg := &specqbft.PrepareData{Data: value}
	encodedPrepare, err := prepareMsg.Encode()
	if err != nil {
		return nil, errors.Wrap(err, "failed to encode prepare data")
	}

	return &specqbft.Message{
		MsgType:    specqbft.PrepareMsgType,
		Height:     i.State().GetHeight(),
		Round:      i.State().GetRound(),
		Identifier: i.State().GetIdentifier(),
		Data:       encodedPrepare,
	}, nil
}

// isPrepared returns true if instance prepared
func (i *Instance) isPrepared() bool {
	return len(i.State().GetPreparedValue()) > 0
}
