package runner

import (
	"fmt"

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

func validatorDutyFromState(state *State) (*spectypes.ValidatorDuty, error) {
	if state == nil {
		return nil, fmt.Errorf("runner state is nil")
	}
	if state.CurrentDuty == nil {
		return nil, fmt.Errorf("current duty is nil")
	}

	validatorDuty, err := validatorDutyFromDuty(state.CurrentDuty)
	if err != nil {
		return nil, fmt.Errorf("current duty: %w", err)
	}

	return validatorDuty, nil
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

func committeeDutyFromState(state *State) (*spectypes.CommitteeDuty, error) {
	if state == nil {
		return nil, fmt.Errorf("runner state is nil")
	}
	if state.CurrentDuty == nil {
		return nil, fmt.Errorf("current duty is nil")
	}

	committeeDuty, err := committeeDutyFromDuty(state.CurrentDuty)
	if err != nil {
		return nil, fmt.Errorf("current duty: %w", err)
	}

	return committeeDuty, nil
}

func validatorConsensusDataFromEncoder(value spectypes.Encoder) (*spectypes.ValidatorConsensusData, error) {
	if value == nil {
		return nil, fmt.Errorf("decided value is nil")
	}

	consensusData, ok := value.(*spectypes.ValidatorConsensusData)
	if !ok {
		return nil, fmt.Errorf("decided value is not a ValidatorConsensusData: %T", value)
	}
	if consensusData == nil {
		return nil, fmt.Errorf("validator consensus data is nil")
	}

	return consensusData, nil
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
