package types

import (
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

const (
	RoleAggregator                = spectypes.RunnerRole(1) // Deprecated
	RoleSyncCommitteeContribution = spectypes.RunnerRole(3) // Deprecated
)

// RunnerRoleForValidatorDuty resolves the runner role for validator duties,
// mapping Alan fork aggregator duties to Alan runner roles.
//
// TODO(convergence unit 5): thread real fork bit instead of a literal false.
func RunnerRoleForValidatorDuty(duty *spectypes.ValidatorDuty, isBooleFork bool) spectypes.RunnerRole {
	if duty == nil {
		return spectypes.RoleUnknown
	}
	if isBooleFork {
		return duty.RunnerRole()
	}

	switch duty.Type {
	case spectypes.BNRoleAggregator:
		return RoleAggregator
	case spectypes.BNRoleSyncCommitteeContribution:
		return RoleSyncCommitteeContribution
	default:
		return duty.RunnerRole()
	}
}

// RunnerRoleForDuty resolves the runner role for any duty using fork context.
func RunnerRoleForDuty(duty spectypes.Duty, isBooleFork bool) spectypes.RunnerRole {
	if duty == nil {
		return spectypes.RoleUnknown
	}
	if vd, ok := duty.(*spectypes.ValidatorDuty); ok {
		return RunnerRoleForValidatorDuty(vd, isBooleFork)
	}
	return duty.RunnerRole()
}
