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

// RunnerRoleToString is a workaround for Alan runner roles.
// Deprecated: use (spectypes.RunnerRole).String() after the Boole fork
func RunnerRoleToString(r spectypes.RunnerRole) string {
	switch r {
	case RoleAggregator:
		return "AGGREGATOR"
	case RoleSyncCommitteeContribution:
		return "SYNC_COMMITTEE_CONTRIBUTION"
	default:
		return r.String()
	}
}
