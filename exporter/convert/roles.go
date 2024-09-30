package convert

import "fmt"

type RunnerRole int32

const (
	RoleAttester RunnerRole = iota
	RoleAggregator
	RoleProposer
	RoleSyncCommitteeContribution
	RoleSyncCommittee

	RoleValidatorRegistration
	RoleVoluntaryExit
	RoleCommittee
)

// String returns name of the runner role
func (r RunnerRole) String() string {
	switch r {
	case RoleAttester:
		return "ATTESTER"
	case RoleAggregator:
		return "AGGREGATOR"
	case RoleProposer:
		return "PROPOSER"
	case RoleSyncCommittee:
		return "SYNC_COMMITTEE"
	case RoleSyncCommitteeContribution:
		return "SYNC_COMMITTEE_CONTRIBUTION"
	case RoleValidatorRegistration:
		return "VALIDATOR_REGISTRATION"
	case RoleVoluntaryExit:
		return "VOLUNTARY_EXIT"
	case RoleCommittee:
		return "COMMITTEE"
	default:
		return "UNDEFINED"
	}
}

// ToBeaconRole returns name of the beacon role
func (r RunnerRole) ToBeaconRole() string {
	switch r {
	case RoleAttester:
		return "ATTESTER"
	case RoleAggregator:
		return "AGGREGATOR"
	case RoleProposer:
		return "PROPOSER"
	case RoleSyncCommittee:
		return "SYNC_COMMITTEE"
	case RoleSyncCommitteeContribution:
		return "SYNC_COMMITTEE_CONTRIBUTION"
	case RoleValidatorRegistration:
		return "VALIDATOR_REGISTRATION"
	case RoleVoluntaryExit:
		return "VOLUNTARY_EXIT"
	default:
		return "UNDEFINED"
	}
}

func RunnerRoleFromString(value string) (RunnerRole, error) {
	switch value {
	case "ATTESTER":
		return RoleAttester, nil
	case "AGGREGATOR":
		return RoleAggregator, nil
	case "PROPOSER":
		return RoleProposer, nil
	case "SYNC_COMMITTEE":
		return RoleSyncCommittee, nil
	case "SYNC_COMMITTEE_CONTRIBUTION":
		return RoleSyncCommitteeContribution, nil
	case "VALIDATOR_REGISTRATION":
		return RoleValidatorRegistration, nil
	case "VOLUNTARY_EXIT":
		return RoleVoluntaryExit, nil
	case "COMMITTEE":
		return RoleCommittee, nil
	default:
		return RoleCommittee, fmt.Errorf("invalid runner role: %s", value)
	}
}
