package ibft

import (
	"bytes"
	"encoding/hex"
	"encoding/json"

	"github.com/bloxapp/ssv/ibft/proto"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

func (i *Instance) changeRoundMsgPipeline() Pipeline {
	return []PipelineFunc{
		MsgTypeCheck(proto.RoundState_ChangeRound),
		i.ValidateLambdas(),
		i.ValidateRound(), // TODO - should we validate round? or maybe just higher round?
		i.AuthMsg(),
		i.validateChangeRoundMsg(),
		i.addChangeRoundMessage(),
		i.uponChangeRoundPartialQuorum(),
		i.uponChangeRoundFullQuorum(),
	}
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
func (i *Instance) highestPrepared(round uint64) (changeData *proto.ChangeRoundData, err error) {
	for _, msg := range i.changeRoundMessages.ReadOnlyMessagesByRound(round) {
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
	for _, msg := range i.changeRoundMessages.ReadOnlyMessagesByRound(round) {
		if msg.Message.Value == nil {
			cnt++
		}
	}

	quorum := cnt*3 >= i.params.CommitteeSize()*2
	if quorum { // quorum for prj = ⊥ ∧ pvj = ⊥ found
		return true, nil
	} else {
		data, err := i.highestPrepared(round)
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
}

func (i *Instance) validateChangeRoundMsg() PipelineFunc {
	return func(signedMessage *proto.SignedMessage) error {
		// msg.value holds the justification value in a change round message
		if signedMessage.Message.Value != nil {
			data := &proto.ChangeRoundData{}
			if err := json.Unmarshal(signedMessage.Message.Value, data); err != nil {
				return err
			}
			if data.JustificationMsg.Type != proto.RoundState_Prepare {
				return errors.New("change round justification msg type not Prepare")
			}
			if signedMessage.Message.Round <= data.JustificationMsg.Round {
				return errors.New("change round justification round lower or equal to message round")
			}
			if data.PreparedRound != data.JustificationMsg.Round {
				return errors.New("change round prepared round not equal to justification msg round")
			}
			if !bytes.Equal(signedMessage.Message.Lambda, data.JustificationMsg.Lambda) {
				return errors.New("change round justification msg lambda not equal to msg lambda")
			}
			if !bytes.Equal(data.PreparedValue, data.JustificationMsg.Value) {
				return errors.New("change round prepared value not equal to justification msg value")
			}

			// validate signature
			// TODO - validate signed ids are unique
			pks, err := i.params.PubKeysById(data.SignedIds)
			if err != nil {
				return err
			}
			aggregated := pks.Aggregate()
			res, err := data.VerifySig(aggregated)
			if err != nil {
				return err
			}
			if !res {
				return errors.New("change round justification signature doesn't verify")
			}

		}
		return nil
	}
}

func (i *Instance) roundChangeInputValue() ([]byte, error) {
	if i.State.PreparedRound != 0 { // TODO is this safe? should we have a flag indicating we prepared?
		batched := i.batchedPrepareMsgs(i.State.PreparedRound)
		msgs := batched[hex.EncodeToString(i.State.PreparedValue)]

		// set justificationMsg and sig
		var justificationMsg *proto.Message
		var aggregatedSig *bls.Sign
		ids := make([]uint64, 0)
		if len(msgs)*3 >= i.params.CommitteeSize()*2 {
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
				ids = append(ids, msg.IbftId)
			}
		} else {
			return nil, errors.New("prepared value/ round is set but no quorum of prepare messages found")
		}

		data := &proto.ChangeRoundData{
			PreparedRound:    i.State.PreparedRound,
			PreparedValue:    i.State.PreparedValue,
			JustificationMsg: justificationMsg,
			JustificationSig: aggregatedSig.Serialize(),
			SignedIds:        ids,
		}

		return json.Marshal(data)
	}
	return nil, nil // not previously prepared
}

func (i *Instance) uponChangeRoundTrigger() {
	// bump round
	i.BumpRound(i.State.Round + 1)
	i.Log("round timeout, changing round", false, zap.Uint64("round", i.State.Round))

	// set time for next round change
	i.triggerRoundChangeOnTimer()

	// broadcast round change
	data, err := i.roundChangeInputValue()
	if err != nil {
		i.Log("failed to create round change data for round", true, zap.Uint64("round", i.State.Round), zap.Error(err))
	}
	broadcastMsg := &proto.Message{
		Type:   proto.RoundState_ChangeRound,
		Round:  i.State.Round,
		Lambda: i.State.Lambda,
		Value:  data,
	}
	if err := i.SignAndBroadcast(broadcastMsg); err != nil {
		i.Log("could not broadcast round change message", true, zap.Error(err))
	}

	// mark stage
	i.SetStage(proto.RoundState_ChangeRound)
}

func (i *Instance) existingChangeRoundMsg(signedMessage *proto.SignedMessage) bool {
	// TODO - not sure the spec requires unique votes.
	msgs := i.changeRoundMessages.ReadOnlyMessagesByRound(signedMessage.Message.Round)
	if _, found := msgs[signedMessage.IbftId]; found {
		return true
	}
	return false
}

// TODO - passing round can be problematic if the node goes down, it might not know which round it is now.
func (i *Instance) changeRoundQuorum(round uint64) (quorum bool, t int, n int) {
	// TODO - should we check the actual change round msg? what if there are no 2/3 change round with the same value?
	msgs := i.changeRoundMessages.ReadOnlyMessagesByRound(round)
	quorum = len(msgs)*3 >= i.params.CommitteeSize()*2
	return quorum, len(msgs), i.params.CommitteeSize()
}

func (i *Instance) addChangeRoundMessage() PipelineFunc {
	return func(signedMessage *proto.SignedMessage) error {
		// TODO - if instance decidedChan should we process round change?
		if i.State.Stage == proto.RoundState_Decided {
			// TODO - can't get here, fails on round verification in pipeline
			i.Log("received change round after decision, sending decidedChan message", false)
			return nil
		}

		// Only 1 prepare per node per round is valid
		if i.existingChangeRoundMsg(signedMessage) {
			return nil
		}

		// add to prepare messages
		i.changeRoundMessages.AddMessage(*signedMessage)
		i.Log("received valid change round message for round", false, zap.Uint64("ibft_id", signedMessage.IbftId), zap.Uint64("round", signedMessage.Message.Round))
		return nil
	}
}

/**
upon receiving a set Frc of f + 1 valid ⟨ROUND-CHANGE, λi, rj, −, −⟩ messages such that:
	∀⟨ROUND-CHANGE, λi, rj, −, −⟩ ∈ Frc : rj > ri do
		let ⟨ROUND-CHANGE, hi, rmin, −, −⟩ ∈ Frc such that:
			∀⟨ROUND-CHANGE, λi, rj, −, −⟩ ∈ Frc : rmin ≤ rj
		ri ← rmin
		set timer i to running and expire after t(ri)
		broadcast ⟨ROUND-CHANGE, λi, ri, pri, pvi⟩
*/
func (i *Instance) uponChangeRoundPartialQuorum() PipelineFunc {
	return func(signedMessage *proto.SignedMessage) error {
		return nil // TODO
	}
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
func (i *Instance) uponChangeRoundFullQuorum() PipelineFunc {
	// TODO - concurrency lock?
	return func(signedMessage *proto.SignedMessage) error {
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
		if quorum {
			i.SetStage(proto.RoundState_PrePrepare)
			i.Log("change round quorum received.", false, zap.Uint64("round", signedMessage.Message.Round), zap.Bool("is_leader", isLeader), zap.Bool("round_justified", justifyRound))

			if isLeader && justifyRound {
				var value []byte
				highest, err := i.highestPrepared(signedMessage.Message.Round)
				if err != nil {
					return err
				}
				if highest != nil {
					value = highest.PreparedValue
					i.Log("broadcasting pre-prepare as leader after round change with existing prepared", false, zap.Uint64("round", signedMessage.Message.Round))

				} else {
					value = i.State.InputValue
					i.Log("broadcasting pre-prepare as leader after round change with input value", false, zap.Uint64("round", signedMessage.Message.Round))
				}

				// send pre-prepare msg
				broadcastMsg := &proto.Message{
					Type:   proto.RoundState_PrePrepare,
					Round:  signedMessage.Message.Round,
					Lambda: i.State.Lambda,
					Value:  value,
				}
				if err := i.SignAndBroadcast(broadcastMsg); err != nil {
					i.Log("could not broadcast pre-prepare message after round change", true, zap.Error(err))
					return err
				}
			}
			return nil
		}

		return nil
	}
}
