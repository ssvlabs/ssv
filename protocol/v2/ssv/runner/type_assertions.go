package runner

import (
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
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

// decidedAttestationVote extracts the committee runner's decided consensus value as the common
// BeaconVote plus, on Gloas-and-later slots, the payload-status index it carries (SIP #94 §2). The
// concrete type — fixed by the decode prototype in ProcessConsensus — selects the fork: a
// GloasBeaconVote yields a non-nil index, a plain BeaconVote a nil one. The BeaconVote half
// (BlockRoot/Source/Target) is identical across forks, so the attestation and sync-committee paths
// consume it unchanged.
func decidedAttestationVote(value spectypes.Encoder) (*spectypes.BeaconVote, *phase0.CommitteeIndex, error) {
	switch v := value.(type) {
	case *gloas.GloasBeaconVote:
		if v == nil {
			return nil, nil, fmt.Errorf("gloas beacon vote is nil")
		}
		index := v.AttestationDataIndex
		return &spectypes.BeaconVote{BlockRoot: v.BlockRoot, Source: v.Source, Target: v.Target}, &index, nil
	case *spectypes.BeaconVote:
		if v == nil {
			return nil, nil, fmt.Errorf("beacon vote is nil")
		}
		return v, nil, nil
	case nil:
		return nil, nil, fmt.Errorf("decided value is nil")
	default:
		return nil, nil, fmt.Errorf("decided value is not a beacon vote: %T", value)
	}
}
