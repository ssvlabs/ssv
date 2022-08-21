package instance

import (
	"encoding/json"
	"fmt"
	"github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"math"
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"

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
		pipelines.WrapFunc("add change round msg", func(signedMessage *specqbft.SignedMessage) error {
			i.Logger.Info("received valid change round message for round",
				zap.Any("sender_ibft_id", signedMessage.GetSigners()),
				zap.Any("msg", signedMessage.Message),
				zap.Uint64("round", uint64(signedMessage.Message.Round)))

			changeRoundData, err := signedMessage.Message.GetRoundChangeData()
			if err != nil {
				return err
			}
			i.containersMap[specqbft.RoundChangeMsgType].AddMessage(signedMessage, changeRoundData.PreparedValue)

			if err := UpdateChangeRoundMessage(i.Logger, i.changeRoundStore, signedMessage); err != nil {
				i.Logger.Warn("failed to update change round msg in storage", zap.Error(err))
			}
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
	return pipelines.WrapFunc("upon change round full quorum", func(signedMessage *specqbft.SignedMessage) error {
		var err error
		quorum, msgsCount, committeeSize := i.changeRoundQuorum(signedMessage.Message.Round)

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
			i.ProcessStageChange(qbft.RoundStateNotStarted)
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
				logger.Info("broadcasting pre-prepare as leader after round change with input value", zap.String("value", fmt.Sprintf("%x", value)))
			} else {
				value = highest.PreparedValue
				logger.Info("broadcasting pre-prepare as leader after round change with justified prepare value", zap.String("value", fmt.Sprintf("%x", value)))
			}

			// send pre-prepare msg
			var broadcastMsg specqbft.Message
			broadcastMsg, err = i.generatePrePrepareMessage(value)
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
func (i *Instance) actOnExistingPrePrepare(signedMessage *specqbft.SignedMessage) error {
	found, msg, err := i.checkExistingPrePrepare(signedMessage.Message.Round)
	if err != nil {
		return err
	}
	if !found {
		i.Logger.Debug("not found exist pre-prepare for change round")
		return nil
	}
	return i.UponPrePrepareMsg().Run(msg)
}

func (i *Instance) changeRoundQuorum(round specqbft.Round) (quorum bool, t int, n int) {
	// TODO - calculate quorum one way (for prepare, commit, change round and decided) and refactor
	msgs := i.containersMap[specqbft.RoundChangeMsgType].ReadOnlyMessagesByRound(round)
	quorum = len(msgs)*3 >= i.ValidatorShare.CommitteeSize()*2
	return quorum, len(msgs), i.ValidatorShare.CommitteeSize()
}

func (i *Instance) roundChangeInputValue() ([]byte, error) {
	// prepare justificationMsg and sig
	var justificationMsg *specqbft.Message
	var aggSig []byte
	var roundChangeJustification []*specqbft.SignedMessage
	ids := make([]spectypes.OperatorID, 0)
	if i.isPrepared() {
		quorum, msgs := i.containersMap[specqbft.PrepareMsgType].QuorumAchieved(i.State().GetPreparedRound(), i.State().GetPreparedValue())
		i.Logger.Debug("change round - checking quorum", zap.Bool("quorum", quorum), zap.Int("msgs", len(msgs)), zap.Any("state", i.State()))
		var aggregatedSig *bls.Sign
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

		roundChangeJustification = []*specqbft.SignedMessage{{
			Signature: aggSig,
			Signers:   ids,
			Message:   justificationMsg,
		}}
	}

	data := &specqbft.RoundChangeData{
		PreparedValue:            i.State().GetPreparedValue(),
		PreparedRound:            i.State().GetPreparedRound(),
		RoundChangeJustification: roundChangeJustification,
	}
	return json.Marshal(data)
}

func (i *Instance) uponChangeRoundTrigger() {
	i.Logger.Info("round timeout, changing round", zap.Uint64("round", uint64(i.State().GetRound())))
	// bump round
	i.BumpRound()
	// mark stage
	i.ProcessStageChange(qbft.RoundStateChangeRound)
}

// BroadcastChangeRound will broadcast a change round message.
func (i *Instance) BroadcastChangeRound() error {
	broadcastMsg, err := i.generateChangeRoundMessage()
	if err != nil {
		return err
	}
	if err := i.SignAndBroadcast(broadcastMsg); err != nil {
		return err
	}
	return nil
}

// JustifyRoundChange see below
func (i *Instance) JustifyRoundChange(round specqbft.Round) error {
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
func (i *Instance) HighestPrepared(round specqbft.Round) (notPrepared bool, highestPrepared *specqbft.RoundChangeData, err error) {
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
	for _, msg := range i.containersMap[specqbft.RoundChangeMsgType].ReadOnlyMessagesByRound(round) {
		candidateChangeData, err := msg.Message.GetRoundChangeData()
		if err != nil {
			return false, nil, err
		}

		// compare to highest found
		if candidateChangeData.PreparedValue != nil && len(candidateChangeData.PreparedValue) > 0 {
			notPrepared = false
			if highestPrepared != nil {
				if candidateChangeData.PreparedRound > highestPrepared.PreparedRound {
					highestPrepared = candidateChangeData
				}
			} else {
				highestPrepared = candidateChangeData
			}
		}
	}
	return notPrepared, highestPrepared, nil
}

func (i *Instance) generateChangeRoundMessage() (*specqbft.Message, error) {
	roundChange, err := i.roundChangeInputValue()
	if err != nil {
		return nil, errors.Wrap(err, "failed to create round change data for round")
	}
	identifier := i.State().GetIdentifier()
	return &specqbft.Message{
		MsgType:    specqbft.RoundChangeMsgType,
		Height:     i.State().GetHeight(),
		Round:      i.State().GetRound(),
		Identifier: identifier[:],
		Data:       roundChange,
	}, nil
}

func (i *Instance) roundTimeoutSeconds() time.Duration {
	roundTimeout := math.Pow(float64(i.Config.RoundChangeDurationSeconds), float64(i.State().GetRound()))
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
