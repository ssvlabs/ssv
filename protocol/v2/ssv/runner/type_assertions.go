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
	if b == nil {
		return nil, fmt.Errorf("runner is nil")
	}
	if b.State == nil {
		return nil, fmt.Errorf("runner state is nil")
	}
	if b.State.CurrentDuty == nil {
		return nil, fmt.Errorf("current duty is nil")
	}

	return validatorDutyFromDuty(b.State.CurrentDuty)
}

func (b *BaseRunner) currentDutySlot() (phase0.Slot, error) {
	if b == nil {
		return 0, fmt.Errorf("runner is nil")
	}
	if b.State == nil {
		return 0, fmt.Errorf("runner state is nil")
	}
	if b.State.CurrentDuty == nil {
		return 0, fmt.Errorf("current duty is nil")
	}

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
