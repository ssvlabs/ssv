package runner

import (
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type Committee struct {
	Runners                 map[phase0.Slot]*CommitteeRunner
	Operator                spectypes.Operator
	SignatureVerifier       spectypes.SignatureVerifier
	CreateRunnerFn          func() *CommitteeRunner
	HighestAttestingSlotMap map[spectypes.ValidatorPK]phase0.Slot
}

// NewCommittee creates a new cluster
func NewCommittee(
	operator spectypes.Operator,
	verifier spectypes.SignatureVerifier,
	createRunnerFn func() *CommitteeRunner,
) *Committee {
	return &Committee{
		Runners:           make(map[phase0.Slot]*CommitteeRunner),
		Operator:          operator,
		SignatureVerifier: verifier,
		CreateRunnerFn:    createRunnerFn,
	}

}

// StartDuty starts a new duty for the given slot
func (c *Committee) StartDuty(logger *zap.Logger, duty *spectypes.CommitteeDuty) error {
	if _, exists := c.Runners[duty.Slot]; exists {
		return errors.New(fmt.Sprintf("CommitteeRunner for slot %d already exists", duty.Slot))
	}
	c.Runners[duty.Slot] = c.CreateRunnerFn()
	var validatorToStopMap map[phase0.Slot]spectypes.ValidatorPK
	// Filter old duties based on highest attesting slot
	duty, validatorToStopMap, c.HighestAttestingSlotMap = FilterCommitteeDuty(duty, c.HighestAttestingSlotMap)
	// Stop validators with old duties
	c.stopDuties(validatorToStopMap)
	c.updateAttestingSlotMap(duty)
	return c.Runners[duty.Slot].StartNewDuty(logger, duty)
}

func (c *Committee) stopDuties(validatorToStopMap map[phase0.Slot]spectypes.ValidatorPK) {
	for slot, validator := range validatorToStopMap {
		runner, exists := c.Runners[slot]
		if exists {
			runner.StopDuty(validator)
		}
	}
}

// FilterCommitteeDuty filters the committee duties by the slots given per validator.
// It returns the filtered duties, the validators to stop and updated slot map.
func FilterCommitteeDuty(duty *spectypes.CommitteeDuty, slotMap map[spectypes.ValidatorPK]phase0.Slot) (
	*spectypes.CommitteeDuty,
	map[phase0.Slot]spectypes.ValidatorPK,
	map[spectypes.ValidatorPK]phase0.Slot,
) {
	validatorsToStop := make(map[phase0.Slot]spectypes.ValidatorPK)

	for i, beaconDuty := range duty.BeaconDuties {
		validatorPK := spectypes.ValidatorPK(beaconDuty.PubKey)
		slot, exists := slotMap[validatorPK]
		if exists {
			if slot < beaconDuty.Slot {
				validatorsToStop[beaconDuty.Slot] = validatorPK
				slotMap[validatorPK] = beaconDuty.Slot
			} else { // else don't run duty with old slot
				duty.BeaconDuties[i] = nil
			}
		}
	}
	return duty, validatorsToStop, slotMap
}

// ProcessMessage processes Network Message of all types
func (c *Committee) ProcessMessage(logger *zap.Logger, signedSSVMessage *spectypes.SignedSSVMessage) error {
	// Validate message
	if err := signedSSVMessage.Validate(); err != nil {
		return errors.Wrap(err, "invalid SignedSSVMessage")
	}

	// Verify SignedSSVMessage's signature
	if err := c.SignatureVerifier.Verify(signedSSVMessage, c.Operator.Committee); err != nil {
		return errors.Wrap(err, "SignedSSVMessage has an invalid signature")
	}

	msg := signedSSVMessage.SSVMessage

	switch msg.GetType() {
	case spectypes.SSVConsensusMsgType:
		qbftMsg := &specqbft.Message{}
		if err := qbftMsg.Decode(msg.GetData()); err != nil {
			return errors.Wrap(err, "could not get consensus Message from network Message")
		}
		runner := c.Runners[phase0.Slot(qbftMsg.Height)]
		// TODO: check if runner is nil
		return runner.ProcessConsensus(logger, signedSSVMessage)
	case spectypes.SSVPartialSignatureMsgType:
		pSigMessages := &spectypes.PartialSignatureMessages{}
		if err := pSigMessages.Decode(msg.GetData()); err != nil {
			return errors.Wrap(err, "could not get post consensus Message from network Message")
		}
		if pSigMessages.Type == spectypes.PostConsensusPartialSig {
			runner := c.Runners[pSigMessages.Slot]
			// TODO: check if runner is nil
			return runner.ProcessPostConsensus(logger, pSigMessages)
		}
	default:
		return errors.New("unknown msg")
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
