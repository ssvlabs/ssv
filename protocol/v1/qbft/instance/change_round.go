package instance

import (
	"encoding/json"
	"fmt"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	"math"
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
	"github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/proposal"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/signedmsg"
)

// ChangeRoundMsgPipeline - the main change round msg pipeline
func (i *Instance) ChangeRoundMsgPipeline() pipelines.SignedMessagePipeline {
	validationPipeline := i.ChangeRoundMsgValidationPipeline()
	return pipelines.Combine(
		pipelines.WrapFunc(validationPipeline.Name(), func(signedMessage *specqbft.SignedMessage) error {
			if err := validationPipeline.Run(signedMessage); err != nil {
				return fmt.Errorf("invalid round change message: %w", err)
			}
			return nil
		}),
		pipelines.WrapFunc("add change round msg", func(signedMessage *specqbft.SignedMessage) error {
			i.Logger.Info("received valid change round message for round",
				zap.Any("sender_ibft_id", signedMessage.GetSigners()),
				zap.Any("msg", signedMessage.Message),
				zap.Uint64("round", uint64(signedMessage.Message.Round)))

			changeRoundData, err := signedMessage.Message.GetRoundChangeData()
			if err != nil {
				return err
			}
			i.ContainersMap[specqbft.RoundChangeMsgType].AddMessage(signedMessage, changeRoundData.PreparedValue)

			if err := UpdateChangeRoundMessage(i.Logger, i.changeRoundStore, signedMessage); err != nil {
				i.Logger.Warn("failed to update change round msg in storage", zap.Error(err))
			}
			return nil
		}),
		i.changeRoundFullQuorumMsgPipeline(),
		i.ChangeRoundPartialQuorumMsgPipeline(),
	)
}

// ChangeRoundMsgValidationPipeline - the main change round msg validation pipeline
func (i *Instance) ChangeRoundMsgValidationPipeline() pipelines.SignedMessagePipeline {
	return i.fork.ChangeRoundMsgValidationPipeline(i.ValidatorShare, i.GetState())
}

func (i *Instance) changeRoundFullQuorumMsgPipeline() pipelines.SignedMessagePipeline {
	return pipelines.CombineQuiet(
		signedmsg.ValidateRound(i.GetState().GetRound()),
		i.uponChangeRoundFullQuorum(),
	)
}

/**
upon receiving a quorum Qrc of valid ⟨ROUND-CHANGE, λi, ri, −, −⟩ messages such that
	leader(λi, ri) = pi ∧ JustifyRoundChange(Qrc) do
		if HighestPrepared(Qrc) ̸= ⊥ then
			let v such that (−, v) = HighestPrepared(Qrc))
		else
			let v such that v = inputValue i
		broadcast ⟨PROPOSAL, λi, ri, v⟩
*/
func (i *Instance) uponChangeRoundFullQuorum() pipelines.SignedMessagePipeline {
	return pipelines.WrapFunc("upon change round full quorum", func(signedMessage *specqbft.SignedMessage) error {
		var err error
		roundChanges := i.ContainersMap[specqbft.RoundChangeMsgType].ReadOnlyMessagesByRound(i.GetState().GetRound())
		quorum, msgsCount, committeeSize := signedmsg.HasQuorum(i.ValidatorShare, roundChanges)

		// change round if quorum reached
		if !quorum {
			i.Logger.Info("change round - quorum not reached",
				zap.Uint64("round", uint64(signedMessage.Message.Round)),
				zap.Int("msgsCount", msgsCount),
				zap.Int("committeeSize", committeeSize),
				zap.Uint64("leader", i.ThisRoundLeader()),
			)
			return nil
		}

		err = i.JustifyRoundChange(signedMessage.Message.Round)
		if err != nil {
			return errors.Wrap(err, "could not justify change round quorum")
		}

		i.processChangeRoundQuorumOnce.Do(func() {
			i.ProcessStageChange(qbft.RoundStateReady)
			logger := i.Logger.With(zap.Uint64("round", uint64(signedMessage.Message.Round)),
				zap.Bool("is_leader", i.IsLeader()),
				zap.Uint64("leader", i.ThisRoundLeader()),
				zap.Bool("round_justified", true))
			logger.Info("change round quorum received")

			if !i.IsLeader() {
				err = i.actOnExistingProposal(signedMessage)
				return
			}

			highest, e := i.HighestPrepared(signedMessage.Message.Round)
			if e != nil {
				err = e
				return
			}

			if highest == nil {
				i.Logger.Warn("no height for leader")
				return
			}

			proposalData := &specqbft.ProposalData{
				RoundChangeJustification: roundChanges,
				PrepareJustification:     highest.RoundChangeJustification,
			}

			if !highest.Prepared() {
				proposalData.Data = i.GetState().GetInputValue()
				logger.Info("broadcasting proposal as leader after round change with input value", zap.String("value", fmt.Sprintf("%x", proposalData.Data)))
			} else {
				proposalData.Data = highest.PreparedValue
				logger.Info("broadcasting proposal as leader after round change with justified prepare value", zap.String("value", fmt.Sprintf("%x", proposalData.Data)))
			}

			logger.Debug("PROPOSAL GENERATE", zap.Any("", proposalData))
			// send proposal msg
			var broadcastMsg specqbft.Message
			broadcastMsg, err = i.GenerateProposalMessage(proposalData)
			if err != nil {
				return
			}
			if e := i.SignAndBroadcast(&broadcastMsg); e != nil {
				logger.Error("could not broadcast proposal message after round change", zap.Error(err))
				err = e
			}
		})
		return err
	})
}

// actOnExistingProposal will try to find exiting proposal msg and run the UponProposalMsg if found.
// We do this in case a future proposal msg was sent before we reached change round quorum, this check is to prevent the instance to wait another round.
func (i *Instance) actOnExistingProposal(signedMessage *specqbft.SignedMessage) error {
	found, msg, err := i.checkExistingProposal(signedMessage.Message.Round)
	if err != nil {
		return err
	}
	if !found {
		i.Logger.Debug("not found exist proposal for change round")
		return nil
	}
	return i.UponProposalMsg().Run(msg)
}

func (i *Instance) roundChangeInputValue() ([]byte, error) {
	// prepare justificationMsg and sig
	data := &specqbft.RoundChangeData{
		PreparedValue: i.GetState().GetPreparedValue(),
		PreparedRound: i.GetState().GetPreparedRound(),
	}
	if i.isPrepared() {
		quorum, msgs := i.ContainersMap[specqbft.PrepareMsgType].QuorumAchieved(i.GetState().GetPreparedRound(), i.GetState().GetPreparedValue())
		i.Logger.Debug("change round - checking quorum", zap.Bool("quorum", quorum), zap.Int("msgs", len(msgs)), zap.Any("state", i.GetState()))
		// temp solution in order to support backwards compatibility. TODO need to remove in version tag v0.3.3
		roundChangeJustification, err := i.handleDeprecatedPrepareJustification(msgs)
		if err != nil {
			return nil, errors.Wrap(err, "failed to get change round justification")
		}
		data.RoundChangeJustification = roundChangeJustification
	}

	return json.Marshal(data)
}

// handleDeprecatedPrepareJustification return aggregated change round justification for v0.3.1 version TODO should be removed in v0.3.3
func (i *Instance) handleDeprecatedPrepareJustification(msgs []*specqbft.SignedMessage) ([]*specqbft.SignedMessage, error) {
	if len(msgs) == 0 {
		return []*specqbft.SignedMessage{}, nil
	}
	var justificationMsg *specqbft.Message
	var aggSig []byte
	var aggregatedSig *bls.Sign
	ids := make([]spectypes.OperatorID, 0)

	justificationMsg = msgs[0].Message
	for _, msg := range msgs {
		// add sig to aggregate
		sig := &bls.Sign{}
		if err := sig.Deserialize(msg.Signature); err != nil {
			return nil, err
		}
		if aggregatedSig == nil {
			aggregatedSig = sig
		} else {
			aggregatedSig.Add(sig)
		}

		// add id to list
		ids = append(ids, msg.GetSigners()...)
	}
	aggSig = aggregatedSig.Serialize()

	return []*specqbft.SignedMessage{{
		Signature: aggSig,
		Signers:   ids,
		Message:   justificationMsg,
	}}, nil
}

func (i *Instance) uponChangeRoundTrigger() {
	i.Logger.Info("round timeout, changing round", zap.Uint64("round", uint64(i.GetState().GetRound())))
	// reset proposal for round
	i.GetState().ProposalAcceptedForCurrentRound.Store((*specqbft.SignedMessage)(nil))
	// bump round
	i.BumpRound()
	// mark stage
	i.ProcessStageChange(qbft.RoundStateChangeRound)
}

// BroadcastChangeRound will broadcast a change round message.
func (i *Instance) BroadcastChangeRound() error {
	broadcastMsg, err := i.GenerateChangeRoundMessage()
	if err != nil {
		return err
	}
	if err := i.SignAndBroadcast(broadcastMsg); err != nil {
		return err
	}
	return nil
}

// JustifyRoundChange see below
// TODO: consider removing
func (i *Instance) JustifyRoundChange(round specqbft.Round) error {
	// ### Algorithm 4 IBFTController pseudocode for process pi: message justification
	//	predicate JustifyRoundChange(Qrc) return
	//		∀⟨ROUND-CHANGE, λi, ri, prj, pvj⟩ ∈ Qrc : prj = ⊥ ∧ pvj = ⊥
	//		∨ received a quorum of valid ⟨PREPARE, λi, pr, pv⟩ messages such that:
	//			(pr, pv) = HighestPrepared(Qrc)

	highest, err := i.HighestPrepared(round)
	if err != nil {
		return err
	}

	if (highest == nil || !highest.Prepared()) && i.isPrepared() {
		return errors.New("highest prepared doesn't match prepared state")
	}

	/**
	IMPORTANT
	Change round msgs are verified against their justifications as well in the pipline, a quorum of change round msgs
	will not include un justified prepared round/ value indicated by a change round msg.
	*/

	return nil
}

// HighestPrepared is slightly changed to also include a returned flag to indicate if all change round messages have prj = ⊥ ∧ pvj = ⊥
func (i *Instance) HighestPrepared(round specqbft.Round) (highestPrepared *specqbft.RoundChangeData, err error) {
	/**
	### Algorithm 4 IBFTController pseudocode for process pi: message justification
		Helper function that returns a tuple (pr, pv) where pr and pv are, respectively,
		the prepared round and the prepared value of the ROUND-CHANGE message in Qrc with the highest prepared round.
		function HighestPrepared(Qrc)
			return (pr, pv) such that:
				∃⟨ROUND-CHANGE, λi, round, pr, pv⟩ ∈ Qrc :
					∀⟨ROUND-CHANGE, λi, round, prj, pvj⟩ ∈ Qrc : prj = ⊥ ∨ pr ≥ prj
	*/

	roundChanges := i.ContainersMap[specqbft.RoundChangeMsgType].ReadOnlyMessagesByRound(i.GetState().GetRound())
	for _, msg := range roundChanges {
		candidateChangeData, err := msg.Message.GetRoundChangeData()
		if err != nil {
			return nil, err
		}

		if err := proposal.Justify(i.ValidatorShare, i.GetState(), msg.Message.Round, roundChanges, candidateChangeData.RoundChangeJustification, candidateChangeData.PreparedValue); err != nil {
			i.Logger.Warn("round change not justified", zap.Error(err))
			continue
		}

		noPrevProposal := i.GetState().GetProposalAcceptedForCurrentRound() == nil && i.GetState().GetRound() == round
		prevProposal := i.GetState().GetProposalAcceptedForCurrentRound() != nil && round > i.GetState().GetRound()

		if !noPrevProposal && !prevProposal {
			i.Logger.Warn("round change noPrev or prev", zap.Bool("noPrevProposal", noPrevProposal), zap.Bool("prevProposal", prevProposal))
			continue
		}

		return candidateChangeData, nil
	}
	return nil, nil
}

// GenerateChangeRoundMessage return change round msg
func (i *Instance) GenerateChangeRoundMessage() (*specqbft.Message, error) {
	roundChange, err := i.roundChangeInputValue()
	if err != nil {
		return nil, errors.Wrap(err, "failed to create round change data for round")
	}
	identifier := i.GetState().GetIdentifier()
	return &specqbft.Message{
		MsgType:    specqbft.RoundChangeMsgType,
		Height:     i.GetState().GetHeight(),
		Round:      i.GetState().GetRound(),
		Identifier: identifier[:],
		Data:       roundChange,
	}, nil
}

func (i *Instance) roundTimeoutSeconds() time.Duration {
	roundTimeout := math.Pow(float64(i.Config.RoundChangeDurationSeconds), float64(i.GetState().GetRound()))
	return time.Duration(float64(time.Second) * roundTimeout)
}

// HighestRoundTimeoutSeconds implements Instancer
func (i *Instance) HighestRoundTimeoutSeconds() time.Duration {
	round := i.GetState().GetRound()
	if round < 7 {
		return 0
	}
	roundTimeout := math.Pow(float64(i.Config.RoundChangeDurationSeconds), float64(round-3))
	return time.Duration(float64(time.Second) * roundTimeout)
}

// UpdateChangeRoundMessage if round for specific signer is higher than local msg
func UpdateChangeRoundMessage(logger *zap.Logger, changeRoundStorage qbftstorage.ChangeRoundStore, msg *specqbft.SignedMessage) error {
	local, err := changeRoundStorage.GetLastChangeRoundMsg(msg.Message.Identifier, msg.GetSigners()...) // assume 1 signer
	if err != nil {
		return errors.Wrap(err, "failed to get last change round msg")
	}

	fLogger := logger.With(zap.Any("signers", msg.GetSigners()))

	if len(local) == 0 {
		// no last changeRound msg exist, save the first one
		fLogger.Debug("no last change round exist. saving first one", zap.Int64("NewHeight", int64(msg.Message.Height)), zap.Int64("NewRound", int64(msg.Message.Round)))
		return changeRoundStorage.SaveLastChangeRoundMsg(msg)
	}
	lastMsg := local[0]
	fLogger = fLogger.With(
		zap.Int64("lastHeight", int64(lastMsg.Message.Height)),
		zap.Int64("NewHeight", int64(msg.Message.Height)),
		zap.Int64("lastRound", int64(lastMsg.Message.Round)),
		zap.Int64("NewRound", int64(msg.Message.Round)))

	if msg.Message.Height < lastMsg.Message.Height {
		// height is lower than the last known
		fLogger.Debug("new changeRoundMsg height is lower than last changeRoundMsg")
		return nil
	} else if msg.Message.Height == lastMsg.Message.Height {
		if msg.Message.Round <= lastMsg.Message.Round {
			// round is not higher than last known
			fLogger.Debug("new changeRoundMsg round is lower than last changeRoundMsg")
			return nil
		}
	}

	// new msg is higher than last one, save.
	fLogger.Debug("last change round updated")
	return changeRoundStorage.SaveLastChangeRoundMsg(msg)
}
