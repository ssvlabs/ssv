package message

import spectypes "github.com/bloxapp/ssv-spec/types"

// RoleType type of the validator role for a specific duty
type RoleType int

// String returns name of the role
func (r RoleType) String() string {
	switch r {
	case RoleTypeUnknown:
		return "UNKNOWN"
	case RoleTypeAttester:
		return "ATTESTER"
	case RoleTypeAggregator:
		return "AGGREGATOR"
	case RoleTypeProposer:
		return "PROPOSER"
	default:
		return "UNDEFINED"
	}
}

// RoleTypeFromString returns RoleType from string
func RoleTypeFromString(rt string) spectypes.BeaconRole {
	switch rt {
	case "ATTESTER":
		return spectypes.BNRoleAttester
	case "AGGREGATOR":
		return spectypes.BNRoleAggregator
	case "PROPOSER":
		return spectypes.BNRoleProposer
	default:
		// TODO(nkryuchkov): don't panic
		panic("unknown role")
	}
}

// List of roles
const (
	RoleTypeUnknown RoleType = iota
	RoleTypeAttester
	RoleTypeAggregator
	RoleTypeProposer
)
