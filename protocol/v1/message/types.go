package message

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

func RoleTypeFromString(rt string) RoleType {
	switch rt {
	case "UNKNOWN":
		return RoleTypeUnknown
	case "ATTESTER":
		return RoleTypeAttester
	case "AGGREGATOR":
		return RoleTypeAggregator
	case "PROPOSER":
		return RoleTypeProposer
	default:
		return RoleTypeUnknown
	}
}

// List of roles
const (
	RoleTypeUnknown RoleType = iota
	RoleTypeAttester
	RoleTypeAggregator
	RoleTypeProposer
)
