package ibft

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/herumi/bls-eth-go-binary/bls"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/types"
)

/**
### Algorithm 4 IBFT pseudocode for process pi: message justification
	Helper function that returns a tuple (pr, pv) where pr and pv are, respectively,
	the prepared round and the prepared value of the ROUND-CHANGE message in Qrc with the highest prepared round.
	function HighestPrepared(Qrc)
		return (pr, pv) such that:
			∃⟨ROUND-CHANGE, λi, round, pr, pv⟩ ∈ Qrc :
				∀⟨ROUND-CHANGE, λi, round, prj, pvj⟩ ∈ Qrc : prj = ⊥ ∨ pr ≥ prj
*/
func (i *iBFTInstance) highestPrepared(round uint64) (changeData *types.ChangeRoundData, err error) {
	for _, msg := range i.roundChangeMessages.ReadOnlyMessagesByRound(round) {
		if msg.Message.Value == nil {
			continue
		}
		candidateChangeData := &types.ChangeRoundData{}
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
func (i *iBFTInstance) justifyRoundChange(round uint64) (bool, error) {
	cnt := 0
	// Find quorum for round change messages with prj = ⊥ ∧ pvj = ⊥
	for _, msg := range i.roundChangeMessages.ReadOnlyMessagesByRound(round) {
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
			return false, errors.New("could not justify round change, did not find highest prepared")
		}
		if !i.state.PreviouslyPrepared() { // no previous prepared round
			return false, errors.New("could not justify round change, did not received quorum of prepare messages previously")
		}
		return data.PreparedRound == i.state.PreparedRound &&
			bytes.Equal(data.PreparedValue, i.state.PreparedValue), nil
	}
}

func (i *iBFTInstance) validateChangeRoundMsg() types.PipelineFunc {
	return func(signedMessage *types.SignedMessage) error {
		// msg.value holds the justification value in a change round message
		if signedMessage.Message.Value != nil {
			data := &types.ChangeRoundData{}
			if err := json.Unmarshal(signedMessage.Message.Value, data); err != nil {
				return err
			}
			if data.JustificationMsg.Type != types.RoundState_Prepare {
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
			aggregated := types.PubKeys(pks).Aggregate()
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

func (i *iBFTInstance) roundChangeInputValue() ([]byte, error) {
	if i.state.PreparedRound != 0 { // TODO is this safe? should we have a flag indicating we prepared?
		batched := i.batchedPrepareMsgs(i.state.PreparedRound)
		msgs := batched[hex.EncodeToString(i.state.PreparedValue)]

		// set justificationMsg and sig
		var justificationMsg *types.Message
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

		data := &types.ChangeRoundData{
			PreparedRound:    i.state.PreparedRound,
			PreparedValue:    i.state.PreparedValue,
			JustificationMsg: justificationMsg,
			JustificationSig: aggregatedSig.Serialize(),
			SignedIds:        ids,
		}

		return json.Marshal(data)
	}
	return nil, nil // not previously prepared
}

func (i *iBFTInstance) uponChangeRoundTrigger() {
	i.log.Info("round timeout, changing round", zap.Uint64("round", i.state.Round))

	// bump round
	i.state.Round++

	// set time for next round change
	i.triggerRoundChangeOnTimer()

	// broadcast round change
	data, err := i.roundChangeInputValue()
	if err != nil {
		i.log.Error("failed to create round change data for round", zap.Uint64("round", i.state.Round), zap.Error(err))
	}
	broadcastMsg := &types.Message{
		Type:   types.RoundState_ChangeRound,
		Round:  i.state.Round,
		Lambda: i.state.Lambda,
		Value:  data,
	}
	if err := i.SignAndBroadcast(broadcastMsg); err != nil {
		i.log.Error("could not broadcast round change message", zap.Error(err))
	}

	// mark stage
	i.state.Stage = types.RoundState_ChangeRound
}

func (i *iBFTInstance) uponChangeRoundMsg() types.PipelineFunc {
	return func(signedMessage *types.SignedMessage) error {
		i.log.Info("changing round")
		return nil
	}
}
