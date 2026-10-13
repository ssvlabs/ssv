package casts

import (
	spectypes "github.com/ssvlabs/ssv-spec/types"

	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

func BeaconRoleToRunnerRole(runnerRole spectypes.BeaconRole) spectypes.RunnerRole {
	switch runnerRole {
	case spectypes.BNRoleAttester:
		return spectypes.RoleCommittee
	case spectypes.BNRoleAggregator:
		return ssvtypes.RoleAggregator
	case spectypes.BNRoleProposer:
		return spectypes.RoleProposer
	case spectypes.BNRoleSyncCommittee:
		return spectypes.RoleCommittee
	case spectypes.BNRoleSyncCommitteeContribution:
		return ssvtypes.RoleSyncCommitteeContribution
	case spectypes.BNRoleValidatorRegistration:
		return spectypes.RoleValidatorRegistration
	case spectypes.BNRoleVoluntaryExit:
		return spectypes.RoleVoluntaryExit
	default:
		return spectypes.RoleUnknown
	}
}
