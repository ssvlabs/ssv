package runner

import (
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

func validatorDutyFromDuty(duty spectypes.Duty) (*spectypes.ValidatorDuty, error) {
	if duty == nil {
		return nil, fmt.Errorf("duty is nil")
	}

	validatorDuty, ok := duty.(*spectypes.ValidatorDuty)
	if !ok {
		return nil, fmt.Errorf("duty is not a ValidatorDuty: %T", duty)
	}
	if validatorDuty == nil {
		return nil, fmt.Errorf("validator duty is nil")
	}

	return validatorDuty, nil
}

func (b *BaseRunner) currentValidatorDuty() (*spectypes.ValidatorDuty, error) {
	if !b.hasDutyAssigned() {
		return nil, fmt.Errorf("no current duty assigned")
	}

	// CurrentDuty is not nil if State is not nil by construction.
	return validatorDutyFromDuty(b.State.CurrentDuty)
}

func committeeDutyFromDuty(duty spectypes.Duty) (*spectypes.CommitteeDuty, error) {
	if duty == nil {
		return nil, fmt.Errorf("duty is nil")
	}

	committeeDuty, ok := duty.(*spectypes.CommitteeDuty)
	if !ok {
		return nil, fmt.Errorf("duty is not a CommitteeDuty: %T", duty)
	}
	if committeeDuty == nil {
		return nil, fmt.Errorf("committee duty is nil")
	}

	return committeeDuty, nil
}

func (b *BaseRunner) currentCommitteeDuty() (*spectypes.CommitteeDuty, error) {
	if !b.hasDutyAssigned() {
		return nil, fmt.Errorf("no current duty assigned")
	}

	// CurrentDuty is not nil if State is not nil by construction.
	return committeeDutyFromDuty(b.State.CurrentDuty)
}

func (b *BaseRunner) currentDutySlot() (phase0.Slot, error) {
	if !b.hasDutyAssigned() {
		return 0, fmt.Errorf("no current duty assigned")
	}

	// CurrentDuty is not nil if State is not nil by construction.
	switch duty := b.State.CurrentDuty.(type) {
	case *spectypes.ValidatorDuty:
		if duty == nil {
			return 0, fmt.Errorf("validator duty is nil")
		}
		return duty.DutySlot(), nil
	case *spectypes.CommitteeDuty:
		if duty == nil {
			return 0, fmt.Errorf("committee duty is nil")
		}
		return duty.DutySlot(), nil
	case *spectypes.AggregatorCommitteeDuty:
		if duty == nil {
			return 0, fmt.Errorf("aggregator committee duty is nil")
		}
		return duty.DutySlot(), nil
	default:
		return 0, fmt.Errorf("unsupported duty type: %T", b.State.CurrentDuty)
	}
}

func beaconVoteFromEncoder(value spectypes.Encoder) (*spectypes.BeaconVote, error) {
	if value == nil {
		return nil, fmt.Errorf("decided value is nil")
	}

	beaconVote, ok := value.(*spectypes.BeaconVote)
	if !ok {
		return nil, fmt.Errorf("decided value is not a BeaconVote: %T", value)
	}
	if beaconVote == nil {
		return nil, fmt.Errorf("beacon vote is nil")
	}

	return beaconVote, nil
}
