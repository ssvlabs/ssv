package convert

import "fmt"

// Define constants for the role string values
const (
	AttesterString                  = "ATTESTER"
	AggregatorString                = "AGGREGATOR"
	ProposerString                  = "PROPOSER"
	SyncCommitteeString             = "SYNC_COMMITTEE"
	SyncCommitteeContributionString = "SYNC_COMMITTEE_CONTRIBUTION"
	ValidatorRegistrationString     = "VALIDATOR_REGISTRATION"
	VoluntaryExitString             = "VOLUNTARY_EXIT"
	CommitteeString                 = "COMMITTEE"
	UndefinedString                 = "UNDEFINED"
)

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
		return AttesterString
	case RoleAggregator:
		return AggregatorString
	case RoleProposer:
		return ProposerString
	case RoleSyncCommittee:
		return SyncCommitteeString
	case RoleSyncCommitteeContribution:
		return SyncCommitteeContributionString
	case RoleValidatorRegistration:
		return ValidatorRegistrationString
	case RoleVoluntaryExit:
		return VoluntaryExitString
	case RoleCommittee:
		return CommitteeString
	default:
		return UndefinedString
	}
}

// ToBeaconRole returns name of the beacon role
func (r RunnerRole) ToBeaconRole() string {
	switch r {
	case RoleAttester:
		return AttesterString
	case RoleAggregator:
		return AggregatorString
	case RoleProposer:
		return ProposerString
	case RoleSyncCommittee:
		return SyncCommitteeString
	case RoleSyncCommitteeContribution:
		return SyncCommitteeContributionString
	case RoleValidatorRegistration:
		return ValidatorRegistrationString
	case RoleVoluntaryExit:
		return VoluntaryExitString
	default:
		return UndefinedString
	}
}

// RunnerRoleFromString converts a string to a RunnerRole
func RunnerRoleFromString(value string) (RunnerRole, error) {
	switch value {
	case AttesterString:
		return RoleAttester, nil
	case AggregatorString:
		return RoleAggregator, nil
	case ProposerString:
		return RoleProposer, nil
	case SyncCommitteeString:
		return RoleSyncCommittee, nil
	case SyncCommitteeContributionString:
		return RoleSyncCommitteeContribution, nil
	case ValidatorRegistrationString:
		return RoleValidatorRegistration, nil
	case VoluntaryExitString:
		return RoleVoluntaryExit, nil
	case CommitteeString:
		return RoleCommittee, nil
	default:
		return RoleCommittee, fmt.Errorf("invalid runner role: %s", value)
	}
}
