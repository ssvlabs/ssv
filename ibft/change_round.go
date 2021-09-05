package ibft

import (
	"encoding/json"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"math"
	"time"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/pipeline/auth"
	"github.com/bloxapp/ssv/ibft/pipeline/changeround"
	"github.com/bloxapp/ssv/ibft/proto"
)

func (i *Instance) changeRoundMsgValidationPipeline() pipeline.Pipeline {
	return pipeline.Combine(
		auth.BasicMsgValidation(),
		auth.MsgTypeCheck(proto.RoundState_ChangeRound),
		auth.ValidateLambdas(i.State.Lambda.Get()),
		auth.ValidateSequenceNumber(i.State.SeqNumber.Get()),
		auth.AuthorizeMsg(i.ValidatorShare),
		changeround.Validate(i.ValidatorShare),
	)
}

func (i *Instance) changeRoundFullQuorumMsgPipeline() pipeline.Pipeline {
	return pipeline.Combine(
		i.changeRoundMsgValidationPipeline(),
		pipeline.IfFirstTrueContinueToSecond(
			auth.ValidateRound(i.State.Round.Get()),
			i.uponChangeRoundFullQuorum(),
		),
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
func (i *Instance) uponChangeRoundFullQuorum() pipeline.Pipeline {
	return pipeline.WrapFunc("upon change round full quorum", func(signedMessage *proto.SignedMessage) error {
		var err error
		quorum, msgsCount, committeeSize := i.changeRoundQuorum(signedMessage.Message.Round)

		// change round if quorum reached
		if !quorum {
			i.Logger.Info("change round - quorum not reached", zap.Uint64("round", signedMessage.Message.Round), zap.Int("msgsCount", msgsCount), zap.Int("committeeSize", committeeSize))
			return nil
		}

		justifyRound, err := i.JustifyRoundChange(signedMessage.Message.Round)
		if err != nil {
			return errors.Wrap(err, "could not justify change round quorum")
		}

		i.processChangeRoundQuorumOnce.Do(func() {
			i.ProcessStageChange(proto.RoundState_PrePrepare)
			logger := i.Logger.With(zap.Uint64("round", signedMessage.Message.Round),
				zap.Bool("is_leader", i.IsLeader()),
				zap.Bool("round_justified", justifyRound))
			logger.Info("change round quorum received")

			if !i.IsLeader() {
				return
			}

			notPrepared, highest, e := i.highestPrepared(signedMessage.Message.Round)
			if e != nil {
				err = e
				return
			}

			var value []byte
			if notPrepared {
				value = i.State.InputValue.Get()
				logger.Info("broadcasting pre-prepare as leader after round change with input value")
			} else {
				value = highest.PreparedValue
				logger.Info("broadcasting pre-prepare as leader after round change with justified prepare value")
			}

			// send pre-prepare msg
			broadcastMsg := i.generatePrePrepareMessage(value)
			if e := i.SignAndBroadcast(broadcastMsg); e != nil {
				logger.Error("could not broadcast pre-prepare message after round change", zap.Error(err))
				err = e
			}
		})
		return err
	})
}

func (i *Instance) changeRoundQuorum(round uint64) (quorum bool, t int, n int) {
	// TODO - calculate quorum one way (for prepare, commit, change round and decided) and refactor
	msgs := i.ChangeRoundMessages.ReadOnlyMessagesByRound(round)
	quorum = len(msgs)*3 >= i.ValidatorShare.CommitteeSize()*2
	return quorum, len(msgs), i.ValidatorShare.CommitteeSize()
}

func (i *Instance) roundChangeInputValue() ([]byte, error) {
	// prepare justificationMsg and sig
	var justificationMsg *proto.Message
	var aggSig []byte
	ids := make([]uint64, 0)
	if i.isPrepared() {
		_, msgs := i.PrepareMessages.QuorumAchieved(i.State.PreparedRound.Get(), i.State.PreparedValue.Get())
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
			ids = append(ids, msg.SignerIds...)
		}
		aggSig = aggregatedSig.Serialize()
	}

	data := &proto.ChangeRoundData{
		PreparedRound:    i.State.PreparedRound.Get(),
		PreparedValue:    i.State.PreparedValue.Get(),
		JustificationMsg: justificationMsg,
		JustificationSig: aggSig,
		SignerIds:        ids,
	}

	return json.Marshal(data)
}

func (i *Instance) uponChangeRoundTrigger() {
	i.Logger.Info("round timeout, changing round", zap.Uint64("round", i.State.Round.Get()))
	// bump round
	i.BumpRound()
	// mark stage
	i.ProcessStageChange(proto.RoundState_ChangeRound)

	// set time for next round change
	i.resetRoundTimer()
	// broadcast round change
	if err := i.broadcastChangeRound(); err != nil {
		i.Logger.Error("could not broadcast round change message", zap.Error(err))
	}
}

func (i *Instance) broadcastChangeRound() error {
	broadcastMsg, err := i.generateChangeRoundMessage()
	if err != nil {
		return err
	}
	if err := i.SignAndBroadcast(broadcastMsg); err != nil {
		return err
	}
	i.Logger.Info("broadcasted change round", zap.Uint64("round", broadcastMsg.Round))
	return nil
}

// JustifyRoundChange see below
func (i *Instance) JustifyRoundChange(round uint64) (bool, error) {
	// ### Algorithm 4 IBFT pseudocode for process pi: message justification
	//	predicate JustifyRoundChange(Qrc) return
	//		∀⟨ROUND-CHANGE, λi, ri, prj, pvj⟩ ∈ Qrc : prj = ⊥ ∧ pvj = ⊥
	//		∨ received a quorum of valid ⟨PREPARE, λi, pr, pv⟩ messages such that:
	//			(pr, pv) = HighestPrepared(Qrc)

	notPrepared, _, err := i.highestPrepared(round)
	if err != nil {
		return false, err
	}
	if notPrepared && i.isPrepared() {
		return false, errors.New("highest prepared doesn't match prepared state")
	}

	/**
	IMPORTANT
	Change round msgs are verified against their justifications as well in the pipline, a quorum of change round msgs
	will not include un justified prepared round/ value indicated by a change round msg.
	*/

	return true, nil
}

/**
### Algorithm 4 IBFT pseudocode for process pi: message justification
	Helper function that returns a tuple (pr, pv) where pr and pv are, respectively,
	the prepared round and the prepared value of the ROUND-CHANGE message in Qrc with the highest prepared round.
	function HighestPrepared(Qrc)
		return (pr, pv) such that:
			∃⟨ROUND-CHANGE, λi, round, pr, pv⟩ ∈ Qrc :
				∀⟨ROUND-CHANGE, λi, round, prj, pvj⟩ ∈ Qrc : prj = ⊥ ∨ pr ≥ prj
*/
// highestPrepared is slightly changed to also include a returned flag to indicate if all change round messages have prj = ⊥ ∧ pvj = ⊥
func (i *Instance) highestPrepared(round uint64) (notPrepared bool, highestPrepared *proto.ChangeRoundData, err error) {
	notPrepared = true
	for _, msg := range i.ChangeRoundMessages.ReadOnlyMessagesByRound(round) {
		candidateChangeData := &proto.ChangeRoundData{}
		err = json.Unmarshal(msg.Message.Value, candidateChangeData)
		if err != nil {
			return false, nil, err
		}

		// compare to highest found
		if candidateChangeData.PreparedValue != nil {
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

func (i *Instance) generateChangeRoundMessage() (*proto.Message, error) {
	data, err := i.roundChangeInputValue()
	if err != nil {
		return nil, errors.New("failed to create round change data for round")
	}

	return &proto.Message{
		Type:      proto.RoundState_ChangeRound,
		Round:     i.State.Round.Get(),
		Lambda:    i.State.Lambda.Get(),
		SeqNumber: i.State.SeqNumber.Get(),
		Value:     data,
	}, nil
}

func (i *Instance) roundTimeoutSeconds() time.Duration {
	roundTimeout := math.Pow(float64(i.Config.RoundChangeDurationSeconds), float64(i.State.Round.Get()))
	return time.Duration(float64(time.Second) * roundTimeout)
}
