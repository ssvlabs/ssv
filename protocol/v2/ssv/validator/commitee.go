package validator

import (
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
	"github.com/pkg/errors"
)

// type Entity interface {
// 	SenderID() []byte
// 	PushMessage(msg *queue.DecodedSSVMessage)
// 	UpdateMetadata(metadata *beaconprotocol.ValidatorMetadata)
// 	Stop()
// }

type Committee struct {
	Runners                 map[phase0.Slot]*runner.CommitteeRunner
	Operator                spectypes.Operator
	SignatureVerifier       spectypes.SignatureVerifier
	CreateRunnerFn          func() *runner.CommitteeRunner
	HighestAttestingSlotMap map[spectypes.ValidatorPK]phase0.Slot
}

// StartDuty starts a new duty for the given slot
func (c *Committee) StartDuty(duty *spectypes.CommitteeDuty) error {
	if _, exists := c.Runners[duty.Slot]; exists {
		return errors.New(fmt.Sprintf("CommitteeRunner for slot %d already exists", duty.Slot))
	}
	c.Runners[duty.Slot] = c.CreateRunnerFn()
	validatorToStopMap := make(map[phase0.Slot]spectypes.ValidatorPK)
	// Filter old duties based on highest attesting slot
	duty, validatorToStopMap, c.HighestAttestingSlotMap = FilterCommitteeDuty(duty, c.HighestAttestingSlotMap)
	// Stop validators with old duties
	c.stopDuties(validatorToStopMap)
	c.updateAttestingSlotMap(duty)
	return c.Runners[duty.Slot].StartNewDuty(duty)
}

func (c *Committee) stopDuties(validatorToStopMap map[phase0.Slot]spectypes.ValidatorPK) {
	for slot, validator := range validatorToStopMap {
		runner, exists := c.Runners[slot]
		if exists {
			runner.StopDuty(validator)
		}
	}
}

// FilterCommitteeDuty filters the committee duty. It returns the new duty, the validators to stop and the highest attesting slot map
func FilterCommitteeDuty(duty *spectypes.CommitteeDuty, slotMap map[spectypes.ValidatorPK]phase0.Slot) (
	*spectypes.CommitteeDuty,
	map[phase0.Slot]spectypes.ValidatorPK,
	map[spectypes.ValidatorPK]phase0.Slot) {
	validatorsToStop := make(map[phase0.Slot]spectypes.ValidatorPK)

	for i, beaconDuty := range duty.BeaconDuties {
		validatorPK := spectypes.ValidatorPK(beaconDuty.PubKey)
		slot, exists := slotMap[validatorPK]
		if exists {
			if slot < beaconDuty.Slot {
				validatorsToStop[beaconDuty.Slot] = validatorPK
				slot = beaconDuty.Slot
			} else { // else don't run duty with old slot
				duty.BeaconDuties[i] = nil
			}
		}
	}
	return duty, validatorsToStop, slotMap
}

// ProcessMessage processes Network Message of all types
func (c *Committee) PushMessage(signedSSVMessage *queue.DecodedSSVMessage) error {
	switch signedSSVMessage.GetType() {
	case spectypes.SSVConsensusMsgType:
		qbftMsg := &specqbft.Message{}
		if err := qbftMsg.Decode(signedSSVMessage.GetData()); err != nil {
			return errors.Wrap(err, "could not get consensus Message from network Message")
		}
		runner := c.Runners[phase0.Slot(qbftMsg.Height)]
		// TODO: check if runner is nil
		return runner.ProcessConsensus(signedSSVMessage)
	case spectypes.SSVPartialSignatureMsgType:
		pSigMessages := &spectypes.PartialSignatureMessages{}
		if err := pSigMessages.Decode(signedSSVMessage.GetData()); err != nil {
			return errors.Wrap(err, "could not get post consensus Message from network Message")
		}
		if pSigMessages.Type == spectypes.PostConsensusPartialSig {
			runner := c.Runners[pSigMessages.Slot]
			// TODO: check if runner is nil
			return runner.ProcessPostConsensus(pSigMessages)
		}
	default:
		return errors.New("unknown msg")
	}
	return nil

}

func (c *Committee) validateMessage(msg *spectypes.SSVMessage) error {
	if !c.Operator.ClusterID.MessageIDBelongs(msg.GetID()) {
		return errors.New("Message ID does not match cluster IF")
	}
	if len(msg.GetData()) == 0 {
		return errors.New("msg data is invalid")
	}

	return nil
}

// updateAttestingSlotMap updates the highest attesting slot map from beacon duties
func (c *Committee) updateAttestingSlotMap(duty *spectypes.CommitteeDuty) {
	for _, beaconDuty := range duty.BeaconDuties {
		if beaconDuty.Type == spectypes.BNRoleAttester {
			validatorPK := spectypes.ValidatorPK(beaconDuty.PubKey)
			if c.HighestAttestingSlotMap[validatorPK] < beaconDuty.Slot {
				c.HighestAttestingSlotMap[validatorPK] = beaconDuty.Slot
			}
		}
	}
}
