package instance

import (
	"encoding/json"
	"math"
	"time"

	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/signedmsg"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// ChangeRoundMsgPipeline - the main change round msg pipeline
func (i *Instance) ChangeRoundMsgPipeline() pipelines.SignedMessagePipeline {
	return pipelines.Combine(
		i.ChangeRoundMsgValidationPipeline(),
		pipelines.WrapFunc("add change round msg", func(signedMessage *message.SignedMessage) error {
			i.Logger.Info("received valid change round message for round",
				zap.Any("sender_ibft_id", signedMessage.GetSigners()),
				zap.Uint64("round", uint64(signedMessage.Message.Round)))
			i.ChangeRoundMessages.AddMessage(signedMessage)
			return nil
		}),
		i.ChangeRoundPartialQuorumMsgPipeline(),
		i.changeRoundFullQuorumMsgPipeline(),
	)
}

// ChangeRoundMsgValidationPipeline - the main change round msg validation pipeline
func (i *Instance) ChangeRoundMsgValidationPipeline() pipelines.SignedMessagePipeline {
	return i.fork.ChangeRoundMsgValidationPipeline(i.ValidatorShare, i.State().GetIdentifier(), i.State().GetHeight())
}

func (i *Instance) changeRoundFullQuorumMsgPipeline() pipelines.SignedMessagePipeline {
	return pipelines.CombineQuiet(
		signedmsg.ValidateRound(i.State().GetRound()),
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
		broadcast ⟨PRE-PREPARE, λi, ri, v⟩
*/
func (i *Instance) uponChangeRoundFullQuorum() pipelines.SignedMessagePipeline {
	return pipelines.WrapFunc("upon change round full quorum", func(signedMessage *message.SignedMessage) error {
		var err error
		quorum, msgsCount, committeeSize := i.changeRoundQuorum(signedMessage.Message.Round)

		// change round if quorum reached
		if !quorum {
			i.Logger.Info("change round - quorum not reached",
				zap.Uint64("round", uint64(signedMessage.Message.Round)),
				zap.Int("msgsCount", msgsCount),
				zap.Int("committeeSize", committeeSize),
			)
			return nil
		}

		err = i.JustifyRoundChange(signedMessage.Message.Round)
		if err != nil {
			return errors.Wrap(err, "could not justify change round quorum")
		}

		i.processChangeRoundQuorumOnce.Do(func() {
			i.ProcessStageChange(qbft.RoundState_PrePrepare)
			logger := i.Logger.With(zap.Uint64("round", uint64(signedMessage.Message.Round)),
				zap.Bool("is_leader", i.IsLeader()),
				zap.Uint64("leader", i.ThisRoundLeader()),
				zap.Bool("round_justified", true))
			logger.Info("change round quorum received")

			if !i.IsLeader() {
				err = i.actOnExistingPrePrepare(signedMessage)
				return
			}

			notPrepared, highest, e := i.HighestPrepared(signedMessage.Message.Round)
			if e != nil {
				err = e
				return
			}

			var value []byte
			if notPrepared {
				value = i.State().GetInputValue()
				logger.Info("broadcasting pre-prepare as leader after round change with input value")
			} else {
				value = highest.PreparedValue
				logger.Info("broadcasting pre-prepare as leader after round change with justified prepare value")
			}

			// send pre-prepare msg
			broadcastMsg, err := i.generatePrePrepareMessage(value)
			if err != nil {
				return
			}
			if e := i.SignAndBroadcast(&broadcastMsg); e != nil {
				logger.Error("could not broadcast pre-prepare message after round change", zap.Error(err))
				err = e
			}
		})
		return err
	})
}

// actOnExistingPrePrepare will try to find exiting pre-prepare msg and run the UponPrePrepareMsg if found.
// We do this in case a future pre-prepare msg was sent before we reached change round quorum, this check is to prevent the instance to wait another round.
func (i *Instance) actOnExistingPrePrepare(signedMessage *message.SignedMessage) error {
	found, msg, err := i.checkExistingPrePrepare(signedMessage.Message.Round)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	return i.UponPrePrepareMsg().Run(msg)
}

func (i *Instance) changeRoundQuorum(round message.Round) (quorum bool, t int, n int) {
	// TODO - calculate quorum one way (for prepare, commit, change round and decided) and refactor
	msgs := i.ChangeRoundMessages.ReadOnlyMessagesByRound(round)
	quorum = len(msgs)*3 >= i.ValidatorShare.CommitteeSize()*2
	return quorum, len(msgs), i.ValidatorShare.CommitteeSize()
}

func (i *Instance) roundChangeInputValue() ([]byte, error) {
	// prepare justificationMsg and sig
	var justificationMsg *message.ConsensusMessage
	var aggSig []byte
	ids := make([]message.OperatorID, 0)
	if i.isPrepared() {
		qourum, msgs := i.PrepareMessages.QuorumAchieved(i.State().GetPreparedRound(), i.State().GetPreparedValue())
		i.Logger.Debug("change round - checking quorum", zap.Bool("qourum", qourum), zap.Int("msgs", len(msgs)), zap.Any("state", i.State()))
		var aggregatedSig *bls.Sign
		if len(msgs) == 0 {
			return nil, errors.New("no messages in prepare messages") // TODO on previews version this check was not exist. why we need it now?
		}
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
	}

	data := &message.RoundChangeData{
		PreparedValue:    i.State().GetPreparedValue(),
		Round:            i.State().GetPreparedRound(),
		NextProposalData: nil, // TODO should fill?
		RoundChangeJustification: []*message.SignedMessage{{
			Signature: aggSig,
			Signers:   ids,
			Message:   justificationMsg,
		}},
	}
	return json.Marshal(data)
}

func (i *Instance) uponChangeRoundTrigger() {
	i.Logger.Info("round timeout, changing round", zap.Uint64("round", uint64(i.State().GetRound())))
	// bump round
	i.BumpRound()
	// mark stage
	i.ProcessStageChange(qbft.RoundState_ChangeRound)
}

func (i *Instance) BroadcastChangeRound() error {
	broadcastMsg, err := i.generateChangeRoundMessage()
	if err != nil {
		return err
	}
	if err := i.SignAndBroadcast(broadcastMsg); err != nil {
		return err
	}
	i.Logger.Info("broadcasted change round", zap.Uint64("round", uint64(broadcastMsg.Round)))
	return nil
}

// JustifyRoundChange see below
func (i *Instance) JustifyRoundChange(round message.Round) error {
	// ### Algorithm 4 IBFTController pseudocode for process pi: message justification
	//	predicate JustifyRoundChange(Qrc) return
	//		∀⟨ROUND-CHANGE, λi, ri, prj, pvj⟩ ∈ Qrc : prj = ⊥ ∧ pvj = ⊥
	//		∨ received a quorum of valid ⟨PREPARE, λi, pr, pv⟩ messages such that:
	//			(pr, pv) = HighestPrepared(Qrc)

	notPrepared, _, err := i.HighestPrepared(round)
	if err != nil {
		return err
	}
	if notPrepared && i.isPrepared() {
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
func (i *Instance) HighestPrepared(round message.Round) (notPrepared bool, highestPrepared *message.RoundChangeData, err error) {
	/**
	### Algorithm 4 IBFTController pseudocode for process pi: message justification
		Helper function that returns a tuple (pr, pv) where pr and pv are, respectively,
		the prepared round and the prepared value of the ROUND-CHANGE message in Qrc with the highest prepared round.
		function HighestPrepared(Qrc)
			return (pr, pv) such that:
				∃⟨ROUND-CHANGE, λi, round, pr, pv⟩ ∈ Qrc :
					∀⟨ROUND-CHANGE, λi, round, prj, pvj⟩ ∈ Qrc : prj = ⊥ ∨ pr ≥ prj
	*/

	notPrepared = true
	for _, msg := range i.ChangeRoundMessages.ReadOnlyMessagesByRound(round) {
		candidateChangeData := &message.RoundChangeData{}
		err = json.Unmarshal(msg.Message.Data, candidateChangeData)
		if err != nil {
			return false, nil, err
		}

		// compare to highest found
		if candidateChangeData.PreparedValue != nil {
			notPrepared = false
			if highestPrepared != nil {
				if candidateChangeData.GetPreparedRound() > highestPrepared.GetPreparedRound() {
					highestPrepared = candidateChangeData
				}
			} else {
				highestPrepared = candidateChangeData
			}
		}
	}
	return notPrepared, highestPrepared, nil
}

func (i *Instance) generateChangeRoundMessage() (*message.ConsensusMessage, error) {
	data, err := i.roundChangeInputValue()
	if err != nil {
		return nil, errors.New("failed to create round change data for round")
	}

	return &message.ConsensusMessage{
		MsgType:    message.RoundChangeMsgType,
		Height:     i.State().GetHeight(),
		Round:      i.State().GetRound(),
		Identifier: i.State().GetIdentifier(),
		Data:       data,
	}, nil
}

func (i *Instance) roundTimeoutSeconds() time.Duration {
	roundTimeout := math.Pow(float64(i.Config.RoundChangeDurationSeconds), float64(i.State().GetRound()))
	return time.Duration(float64(time.Second) * roundTimeout)
}
