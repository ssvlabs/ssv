package exporter_message

type RunnerRole int32

const (
	RoleAttester RunnerRole = iota
	RoleAggregator
	RoleProposer
	RoleSyncCommittee
	RoleSyncCommitteeContribution

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
