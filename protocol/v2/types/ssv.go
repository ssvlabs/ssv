package types

import (
	spectypes "github.com/bloxapp/ssv-spec/types"
)

type RunnerRole int32

const (
	RoleCommittee RunnerRole = iota
	RoleAggregator
	RoleProposer
	RoleSyncCommitteeContribution

	RoleValidatorRegistration
	RoleVoluntaryExit

	// Genesis roles.
	RoleAttester
	RoleSyncCommittee

	RoleUnknown = -1
)

func (r RunnerRole) Spec() (role spectypes.RunnerRole, ok bool) {
	switch r {
	case RoleCommittee:
		return spectypes.RoleCommittee, true
	case RoleAggregator:
		return spectypes.RoleAggregator, true
	case RoleProposer:
		return spectypes.RoleProposer, true
	case RoleSyncCommitteeContribution:
		return spectypes.RoleSyncCommitteeContribution, true
	case RoleValidatorRegistration:
		return spectypes.RoleValidatorRegistration, true
	case RoleVoluntaryExit:
		return spectypes.RoleVoluntaryExit, true
	}
	return spectypes.RoleUnknown, false
}

func (r RunnerRole) String() string {
	switch r {
	case RoleCommittee:
		return "COMMITTEE"
	case RoleAggregator:
		return "AGGREGATOR"
	case RoleProposer:
		return "PROPOSER"
	case RoleSyncCommitteeContribution:
		return "SYNC_COMMITTEE_CONTRIBUTION"
	case RoleValidatorRegistration:
		return "VALIDATOR_REGISTRATION"
	case RoleVoluntaryExit:
		return "VOLUNTARY_EXIT"
	case RoleAttester:
		return "ATTESTER"
	case RoleSyncCommittee:
		return "SYNC_COMMITTEE"
	}
	return "UNKNOWN"
}

func RunnerRoleFromString(s string) (RunnerRole, bool) {
	switch s {
	case "COMMITTEE":
		return RoleCommittee, true
	case "AGGREGATOR":
		return RoleAggregator, true
	case "PROPOSER":
		return RoleProposer, true
	case "SYNC_COMMITTEE_CONTRIBUTION":
		return RoleSyncCommitteeContribution, true
	case "VALIDATOR_REGISTRATION":
		return RoleValidatorRegistration, true
	case "VOLUNTARY_EXIT":
		return RoleVoluntaryExit, true
	case "ATTESTER":
		return RoleAttester, true
	case "SYNC_COMMITTEE":
		return RoleSyncCommittee, true
	default:
		return RoleUnknown, false
	}
}

func RunnerRoleFromSpec(role spectypes.RunnerRole) RunnerRole {
	// TODO: fork support
	switch role {
	case spectypes.RoleCommittee:
		return RoleCommittee
	case spectypes.RoleAggregator:
		return RoleAggregator
	case spectypes.RoleProposer:
		return RoleProposer
	case spectypes.RoleSyncCommitteeContribution:
		return RoleSyncCommitteeContribution
	case spectypes.RoleValidatorRegistration:
		return RoleValidatorRegistration
	case spectypes.RoleVoluntaryExit:
		return RoleVoluntaryExit
	default:
		return RoleUnknown
	}
}

func RunnerRoleFromBeacon(role spectypes.BeaconRole) RunnerRole {
	switch role {
	case spectypes.BNRoleAttester:
		return RoleAttester
	case spectypes.BNRoleProposer:
		return RoleProposer
	case spectypes.BNRoleAggregator:
		return RoleAggregator
	case spectypes.BNRoleSyncCommittee:
		return RoleSyncCommittee
	case spectypes.BNRoleSyncCommitteeContribution:
		return RoleSyncCommitteeContribution
	case spectypes.BNRoleValidatorRegistration:
		return RoleValidatorRegistration
	case spectypes.BNRoleVoluntaryExit:
		return RoleVoluntaryExit
	default:
		return RoleUnknown
	}
}
