package message

import (
	"fmt"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

const (
	// SSVEventMsgType extends spec msg type
	SSVEventMsgType spectypes.MsgType = 200

	roleAttester                  = "ATTESTER"
	roleAggregator                = "AGGREGATOR"
	roleProposer                  = "PROPOSER"
	roleSyncCommittee             = "SYNC_COMMITTEE"
	roleSyncCommitteeContribution = "SYNC_COMMITTEE_CONTRIBUTION"
	roleValidatorRegistration     = "VALIDATOR_REGISTRATION"
	roleVoluntaryExit             = "VOLUNTARY_EXIT"
	roleCommittee                 = "COMMITTEE"
	roleAggregatorCommittee       = "AGGREGATOR_COMMITTEE"
)

// MsgTypeToString extension for spec msg type. convert spec msg type to string
func MsgTypeToString(mt spectypes.MsgType) string {
	switch mt {
	case spectypes.SSVConsensusMsgType:
		return "consensus"
	case spectypes.SSVPartialSignatureMsgType:
		return "partial_signature"
	case SSVEventMsgType:
		return "event"
	default:
		return fmt.Sprintf("unknown(%d)", mt)
	}
}

func QBFTMsgTypeToString(mt specqbft.MessageType) string {
	switch mt {
	case specqbft.ProposalMsgType:
		return "proposal"
	case specqbft.PrepareMsgType:
		return "prepare"
	case specqbft.CommitMsgType:
		return "commit"
	case specqbft.RoundChangeMsgType:
		return "round_change"
	default:
		return fmt.Sprintf("unknown(%d)", mt)
	}
}

// BeaconRoleFromString returns BeaconRole from string
func BeaconRoleFromString(s string) (spectypes.BeaconRole, error) {
	switch s {
	case roleAttester:
		return spectypes.BNRoleAttester, nil
	case roleAggregator:
		return spectypes.BNRoleAggregator, nil
	case roleProposer:
		return spectypes.BNRoleProposer, nil
	case roleSyncCommittee:
		return spectypes.BNRoleSyncCommittee, nil
	case roleSyncCommitteeContribution:
		return spectypes.BNRoleSyncCommitteeContribution, nil
	case roleValidatorRegistration:
		return spectypes.BNRoleValidatorRegistration, nil
	case roleVoluntaryExit:
		return spectypes.BNRoleVoluntaryExit, nil
	default:
		return 0, fmt.Errorf("unknown role: %s", s)
	}
}

// RunnerRoleFromString returns RunnerRole from string.
func RunnerRoleFromString(s string) (spectypes.RunnerRole, error) {
	switch s {
	case roleCommittee:
		return spectypes.RoleCommittee, nil
	case roleAggregatorCommittee:
		return spectypes.RoleAggregatorCommittee, nil
	case roleAggregator:
		return ssvtypes.RoleAggregator, nil
	case roleProposer:
		return spectypes.RoleProposer, nil
	case roleSyncCommitteeContribution:
		return ssvtypes.RoleSyncCommitteeContribution, nil
	case roleValidatorRegistration:
		return spectypes.RoleValidatorRegistration, nil
	case roleVoluntaryExit:
		return spectypes.RoleVoluntaryExit, nil
	default:
		return 0, fmt.Errorf("unknown role: %s", s)
	}
}

// CommitteeRunnerRoleFromString returns a committee-backed RunnerRole from
// string. Unlike RunnerRoleFromString it only accepts the two committee runner
// roles (COMMITTEE, AGGREGATOR_COMMITTEE); any other value is rejected so the
// committee-traces filter cannot bind a role the store has no key prefix for.
func CommitteeRunnerRoleFromString(s string) (spectypes.RunnerRole, error) {
	role, err := RunnerRoleFromString(s)
	if err != nil {
		return 0, err
	}
	switch role {
	case spectypes.RoleCommittee, spectypes.RoleAggregatorCommittee:
		return role, nil
	default:
		return 0, fmt.Errorf("unsupported committee runner role: %s", s)
	}
}

func RunnerRoleToString(r spectypes.RunnerRole) string {
	switch r {
	case spectypes.RoleCommittee:
		return roleCommittee
	case spectypes.RoleAggregatorCommittee:
		return roleAggregatorCommittee
	case ssvtypes.RoleAggregator:
		return roleAggregator
	case spectypes.RoleProposer:
		return roleProposer
	case ssvtypes.RoleSyncCommitteeContribution:
		return roleSyncCommitteeContribution
	case spectypes.RoleValidatorRegistration:
		return roleValidatorRegistration
	case spectypes.RoleVoluntaryExit:
		return roleVoluntaryExit
	default:
		return fmt.Sprintf("unknown(%d)", r)
	}
}
