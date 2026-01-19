package types

import (
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

const (
	RoleAggregator                = spectypes.RunnerRole(1) // Deprecated
	RoleSyncCommitteeContribution = spectypes.RunnerRole(3) // Deprecated
)

// RunnerRoleToString is a workaround for Alan runner roles.
// Deprecated: use (spectypes.RunnerRole).String() after the Boole fork
func RunnerRoleToString(r spectypes.RunnerRole) string {
	switch r {
	case RoleAggregator:
		return "AGGREGATOR"
	case RoleSyncCommitteeContribution:
		return "SYNC_COMMITTEE_CONTRIBUTION"
	default:
		return "UNKNOWN"
	}
}
