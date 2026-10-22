package types

import (
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

const (
	RoleAggregator                = spectypes.RunnerRole(1) // Deprecated
	RoleSyncCommitteeContribution = spectypes.RunnerRole(3) // Deprecated
	// RoleValidatorRegistration is the validator-registration runner, retired at Gloas. The
	// Gloas-only ssv-spec removed it but keeps its number reserved; the node runs it until then.
	RoleValidatorRegistration = spectypes.RunnerRole(4)
)

// BNRoleValidatorRegistration is the validator-registration duty, retired at Gloas. The Gloas-only
// ssv-spec removed it but keeps its number reserved; the node schedules it until then.
const BNRoleValidatorRegistration = spectypes.BeaconRole(5)

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
func RunnerRoleForValidatorDuty(duty *spectypes.ValidatorDuty, isBooleFork bool) spectypes.RunnerRole {
	if duty == nil {
		return spectypes.RoleUnknown
	}
	// The Gloas-only ssv-spec no longer maps the retired validator-registration duty.
	if duty.Type == BNRoleValidatorRegistration {
		return RoleValidatorRegistration
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

// RunnerRoleToString is a workaround for runner roles the pinned spec no longer names: the Alan
// roles and the validator-registration role, retired at Gloas, so they are special-cased here.
// message.RunnerRoleToString is an independent mapper that must stay in lockstep — a role
// added or deprecated here must be reflected there (and vice versa).
// Deprecated: use (spectypes.RunnerRole).String() after the Gloas fork, which retires the last of these roles
func RunnerRoleToString(r spectypes.RunnerRole) string {
	switch r {
	case RoleAggregator:
		return "AGGREGATOR"
	case RoleSyncCommitteeContribution:
		return "SYNC_COMMITTEE_CONTRIBUTION"
	case RoleValidatorRegistration:
		return "VALIDATOR_REGISTRATION_RUNNER"
	default:
		return r.String()
	}
}

// BeaconRoleToString is (spectypes.BeaconRole).String() that still names the validator-registration
// duty, which the Gloas-only spec no longer names.
func BeaconRoleToString(r spectypes.BeaconRole) string {
	if r == BNRoleValidatorRegistration {
		return "VALIDATOR_REGISTRATION"
	}

	return r.String()
}
