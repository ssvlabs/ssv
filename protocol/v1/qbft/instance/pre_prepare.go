package instance

import (
	"bytes"
	"fmt"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/signedmsg"
)

// PrePrepareMsgPipeline is the main pre-prepare msg pipeline
func (i *Instance) PrePrepareMsgPipeline() pipelines.SignedMessagePipeline {
	return pipelines.Combine(
		i.prePrepareMsgValidationPipeline(),
		pipelines.WrapFunc("add pre-prepare msg", func(signedMessage *specqbft.SignedMessage) error {
			i.Logger.Info("received valid pre-prepare message for round",
				zap.Any("sender_ibft_id", signedMessage.GetSigners()),
				zap.Uint64("round", uint64(signedMessage.Message.Round)))

			proposalData, err := signedMessage.Message.GetProposalData()
			if err != nil {
				return fmt.Errorf("could not get proposal data: %w", err)
			}
			i.containersMap[specqbft.ProposalMsgType].AddMessage(signedMessage, proposalData.Data)
			return nil
		}),
		pipelines.CombineQuiet(
			signedmsg.ValidateRound(i.State().GetRound()),
			i.UponPrePrepareMsg(),
		),
	)
}

func (i *Instance) prePrepareMsgValidationPipeline() pipelines.SignedMessagePipeline {
	return i.fork.PrePrepareMsgValidationPipeline(i.ValidatorShare, i.State(), i.RoundLeader)
}

// JustifyPrePrepare implements:
// predicate JustifyPrePrepare(hPRE-PREPARE, λi, round, value)
// 	return
// 		round = 1
// 		∨ received a quorum Qrc of valid <ROUND-CHANGE, λi, round, prj , pvj> messages such that:
// 			∀ <ROUND-CHANGE, λi, round, prj , pvj> ∈ Qrc : prj = ⊥ ∧ prj = ⊥
// 			∨ received a quorum of valid <PREPARE, λi, pr, value> messages such that:
// 				(pr, value) = HighestPrepared(Qrc)
func (i *Instance) JustifyPrePrepare(round uint64, preparedValue []byte) error {
	if round == 1 {
		return nil
	}

	if quorum, _, _ := i.changeRoundQuorum(specqbft.Round(round)); quorum {
		notPrepared, highest, err := i.HighestPrepared(specqbft.Round(round))
		if err != nil {
			return err
		}
		if notPrepared {
			return nil
		} else if !bytes.Equal(preparedValue, highest.PreparedValue) {
			return errors.New("preparedValue different than highest prepared")
		}
		return nil
	}
	return errors.New("no change round quorum")
}

/*
UponPrePrepareMsg Algorithm 2 IBFTController pseudocode for process pi: normal case operation
upon receiving a valid ⟨PRE-PREPARE, λi, ri, value⟩ message m from leader(λi, round) such that:
	JustifyPrePrepare(m) do
		set timer i to running and expire after t(ri)
		broadcast ⟨PREPARE, λi, ri, value⟩
*/
func (i *Instance) UponPrePrepareMsg() pipelines.SignedMessagePipeline {
	return pipelines.WrapFunc("upon pre-prepare msg", func(signedMessage *specqbft.SignedMessage) error {
		prepareMsg, err := signedMessage.Message.GetProposalData()
		if err != nil {
			return errors.Wrap(err, "failed to get prepare message")
		}

		// Pre-prepare justification
		err = i.JustifyPrePrepare(uint64(signedMessage.Message.Round), prepareMsg.Data)
		if err != nil {
			return errors.Wrap(err, "Unjustified pre-prepare")
		}

		// mark state
		i.ProcessStageChange(qbft.RoundStatePrePrepare)

		// broadcast prepare msg
		broadcastMsg, err := i.generatePrepareMessage(prepareMsg.Data)
		if err != nil {
			return errors.Wrap(err, "failed to generate prepare message")
		}
		if err := i.SignAndBroadcast(broadcastMsg); err != nil {
			i.Logger.Error("could not broadcast prepare message", zap.Error(err))
			return err
		}
		return nil
	})
}

func (i *Instance) generatePrePrepareMessage(value []byte) (specqbft.Message, error) {
	proposalMsg := &specqbft.ProposalData{
		Data:                     value,
		RoundChangeJustification: nil,
		PrepareJustification:     nil,
	}
	proposalEncodedMsg, err := proposalMsg.Encode()
	if err != nil {
		return specqbft.Message{}, errors.Wrap(err, "failed to encoded proposal message")
	}
	identifier := i.State().GetIdentifier()
	return specqbft.Message{
		MsgType:    specqbft.ProposalMsgType,
		Height:     i.State().GetHeight(),
		Round:      i.State().GetRound(),
		Identifier: identifier[:],
		Data:       proposalEncodedMsg,
	}, nil
}

func (i *Instance) checkExistingPrePrepare(round specqbft.Round) (bool, *specqbft.SignedMessage, error) {
	msgs := i.containersMap[specqbft.ProposalMsgType].ReadOnlyMessagesByRound(round)
	if len(msgs) == 1 {
		return true, msgs[0], nil
	} else if len(msgs) > 1 {
		return false, nil, errors.New("multiple pre-preparer msgs, can't decide which one to use")
	}
	return false, nil, nil
}
