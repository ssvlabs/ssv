package ibft

import (
	"bytes"
	"encoding/hex"
	"encoding/json"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/msgcont"
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/pipeline/auth"
	"github.com/bloxapp/ssv/ibft/pipeline/changeround"
	"github.com/bloxapp/ssv/ibft/proto"
)

func (i *Instance) changeRoundMsgPipeline() pipeline.Pipeline {
	return pipeline.Combine(
		auth.MsgTypeCheck(proto.RoundState_ChangeRound),
		auth.ValidateLambdas(i.State),
		auth.ValidateRound(i.State), // TODO - should we validate round? or maybe just higher round?
		auth.AuthorizeMsg(i.Params),
		changeround.Validate(i.Params),
		changeround.AddChangeRoundMessage(i.Logger, i.ChangeRoundMessages, i.State),
		changeround.UponPartialQuorum(),
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
func (i *Instance) uponChangeRoundFullQuorum() pipeline.Pipeline {
	// TODO - concurrency lock?
	return pipeline.WrapFunc(func(signedMessage *proto.SignedMessage) error {
		if i.State.Stage == proto.RoundState_PrePrepare {
			return nil // no reason to pre-prepare again
		}
		quorum, _, _ := i.changeRoundQuorum(signedMessage.Message.Round)
		justifyRound, err := i.justifyRoundChange(signedMessage.Message.Round)
		if err != nil {
			return err
		}
		isLeader := i.IsLeader()

		// change round if quorum reached
		if !quorum {
			return nil
		}

		i.SetStage(proto.RoundState_PrePrepare)
		i.Logger.Info("change round quorum received.",
			zap.Uint64("round", signedMessage.Message.Round),
			zap.Bool("is_leader", isLeader),
			zap.Bool("round_justified", justifyRound))

		if !isLeader || !justifyRound {
			return nil
		}

		highest, err := highestPrepared(signedMessage.Message.Round, i.ChangeRoundMessages)
		if err != nil {
			return err
		}

		var value []byte
		if highest != nil {
			value = highest.PreparedValue
			i.Logger.Info("broadcasting pre-prepare as leader after round change with existing prepared", zap.Uint64("round", signedMessage.Message.Round))

		} else {
			value = i.State.InputValue
			i.Logger.Info("broadcasting pre-prepare as leader after round change with input value", zap.Uint64("round", signedMessage.Message.Round))
		}

		// send pre-prepare msg
		broadcastMsg := &proto.Message{
			Type:           proto.RoundState_PrePrepare,
			Round:          signedMessage.Message.Round,
			Lambda:         i.State.Lambda,
			PreviousLambda: i.State.PreviousLambda,
			Value:          value,
		}
		if err := i.SignAndBroadcast(broadcastMsg); err != nil {
			i.Logger.Error("could not broadcast pre-prepare message after round change", zap.Error(err))
			return err
		}

		return nil
	})
}

/**
### Algorithm 4 IBFT pseudocode for process pi: message justification
predicate JustifyRoundChange(Qrc) return
	∀⟨ROUND-CHANGE, λi, ri, prj, pvj⟩ ∈ Qrc : prj = ⊥ ∧ pvj = ⊥
	∨ received a quorum of valid ⟨PREPARE, λi, pr, pv⟩ messages such that:
		(pr, pv) = HighestPrepared(Qrc)
*/
func (i *Instance) justifyRoundChange(round uint64) (bool, error) {
	cnt := 0
	// Find quorum for round change messages with prj = ⊥ ∧ pvj = ⊥
	for _, msg := range i.ChangeRoundMessages.ReadOnlyMessagesByRound(round) {
		if msg.Message.Value == nil {
			cnt++
		}
	}

	quorum := cnt*3 >= i.Params.CommitteeSize()*2
	if quorum { // quorum for prj = ⊥ ∧ pvj = ⊥ found
		return true, nil
	}

	data, err := highestPrepared(round, i.ChangeRoundMessages)
	if err != nil {
		return false, err
	}
	if data == nil {
		return true, nil
	}
	if !i.State.PreviouslyPrepared() { // no previous prepared round
		return false, errors.Errorf("could not justify round (%d) change, did not received quorum of prepare messages previously", round)
	}
	return data.PreparedRound == i.State.PreparedRound &&
		bytes.Equal(data.PreparedValue, i.State.PreparedValue), nil
}

// TODO - passing round can be problematic if the node goes down, it might not know which round it is now.
func (i *Instance) changeRoundQuorum(round uint64) (quorum bool, t int, n int) {
	// TODO - should we check the actual change round msg? what if there are no 2/3 change round with the same value?
	msgs := i.ChangeRoundMessages.ReadOnlyMessagesByRound(round)
	quorum = len(msgs)*3 >= i.Params.CommitteeSize()*2
	return quorum, len(msgs), i.Params.CommitteeSize()
}

func (i *Instance) roundChangeInputValue() ([]byte, error) {
	if i.State.PreparedRound != 0 { // TODO is this safe? should we have a flag indicating we prepared?
		batched := i.batchedPrepareMsgs(i.State.PreparedRound)
		msgs := batched[hex.EncodeToString(i.State.PreparedValue)]

		// set justificationMsg and sig
		var justificationMsg *proto.Message
		var aggregatedSig *bls.Sign
		ids := make([]uint64, 0)
		if len(msgs)*3 >= i.Params.CommitteeSize()*2 {
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
		} else {
			return nil, errors.New("prepared value/ round is set but no quorum of prepare messages found")
		}

		data := &proto.ChangeRoundData{
			PreparedRound:    i.State.PreparedRound,
			PreparedValue:    i.State.PreparedValue,
			JustificationMsg: justificationMsg,
			JustificationSig: aggregatedSig.Serialize(),
			SignerIds:        ids,
		}

		return json.Marshal(data)
	}
	return nil, nil // not previously prepared
}

func (i *Instance) uponChangeRoundTrigger() {
	// bump round
	i.BumpRound(i.State.Round + 1)
	i.Logger.Info("round timeout, changing round", zap.Uint64("round", i.State.Round))

	// set time for next round change
	i.triggerRoundChangeOnTimer()

	// broadcast round change
	data, err := i.roundChangeInputValue()
	if err != nil {
		i.Logger.Error("failed to create round change data for round", zap.Uint64("round", i.State.Round), zap.Error(err))
	}
	broadcastMsg := &proto.Message{
		Type:           proto.RoundState_ChangeRound,
		Round:          i.State.Round,
		Lambda:         i.State.Lambda,
		PreviousLambda: i.State.PreviousLambda,
		Value:          data,
	}
	if err := i.SignAndBroadcast(broadcastMsg); err != nil {
		i.Logger.Error("could not broadcast round change message", zap.Error(err))
	}

	// mark stage
	i.SetStage(proto.RoundState_ChangeRound)
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
func highestPrepared(round uint64, container msgcont.MessageContainer) (changeData *proto.ChangeRoundData, err error) {
	for _, msg := range container.ReadOnlyMessagesByRound(round) {
		if msg.Message.Value == nil {
			continue
		}
		candidateChangeData := &proto.ChangeRoundData{}
		err = json.Unmarshal(msg.Message.Value, candidateChangeData)
		if err != nil {
			return nil, err
		}

		// compare to highest found
		if changeData != nil {
			if candidateChangeData.PreparedRound > changeData.PreparedRound {
				changeData = candidateChangeData
			}
		} else {
			changeData = candidateChangeData
		}
	}
	return changeData, nil
}
