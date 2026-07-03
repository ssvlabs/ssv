package types

import (
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

const (
	RoleAggregator                = spectypes.RunnerRole(1) // Deprecated
	RoleSyncCommitteeContribution = spectypes.RunnerRole(3) // Deprecated
)

// CommitteeSignerBucket identifies the committee-duty signer slice that should
// be used for a given beacon role.
type CommitteeSignerBucket uint8

const (
	CommitteeSignerBucketUnknown CommitteeSignerBucket = iota
	CommitteeSignerBucketAttester
	CommitteeSignerBucketSyncCommittee
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

// CommitteeRunnerRoleForBeaconRole maps committee-backed beacon roles to the
// corresponding committee runner role.
func CommitteeRunnerRoleForBeaconRole(role spectypes.BeaconRole) (spectypes.RunnerRole, bool) {
	switch role {
	case spectypes.BNRoleAttester, spectypes.BNRoleSyncCommittee:
		return spectypes.RoleCommittee, true
	case spectypes.BNRoleAggregator, spectypes.BNRoleSyncCommitteeContribution:
		return spectypes.RoleAggregatorCommittee, true
	default:
		return spectypes.RoleUnknown, false
	}
}

// CommitteeSignerBucketForBeaconRole maps committee-backed beacon roles to the
// signer bucket used in CommitteeDutyTrace.
func CommitteeSignerBucketForBeaconRole(role spectypes.BeaconRole) (CommitteeSignerBucket, bool) {
	switch role {
	case spectypes.BNRoleAttester, spectypes.BNRoleAggregator:
		return CommitteeSignerBucketAttester, true
	case spectypes.BNRoleSyncCommittee, spectypes.BNRoleSyncCommitteeContribution:
		return CommitteeSignerBucketSyncCommittee, true
	default:
		return CommitteeSignerBucketUnknown, false
	}
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
