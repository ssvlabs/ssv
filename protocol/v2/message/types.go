package message

import spectypes "github.com/bloxapp/ssv-spec/types"

// RoleTypeFromString returns RoleType from string
func RoleTypeFromString(rt string) spectypes.BeaconRole {
	switch rt {
	case "ATTESTER":
		return spectypes.BNRoleAttester
	case "AGGREGATOR":
		return spectypes.BNRoleAggregator
	case "PROPOSER":
		return spectypes.BNRoleProposer
	case "SYNC_COMMITTEE":
		return spectypes.BNRoleSyncCommittee
	case "SYNC_COMMITTEE_CONTRIBUTION":
		return spectypes.BNRoleSyncCommitteeContribution
	default:
		panic("unknown role")
	}
}
