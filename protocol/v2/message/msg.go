package message

import (
	"fmt"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

const (
	// SSVSyncMsgType extends spec msg type
	SSVSyncMsgType spectypes.MsgType = 100
	// SSVEventMsgType extends spec msg type
	SSVEventMsgType spectypes.MsgType = 200
)

// MsgTypeToString extension for spec msg type. convert spec msg type to string
func MsgTypeToString(mt spectypes.MsgType) string {
	switch mt {
	case spectypes.SSVConsensusMsgType:
		return "consensus"
	case spectypes.SSVPartialSignatureMsgType:
		return "partial_signature"
	case SSVSyncMsgType:
		return "sync"
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
	case "ATTESTER":
		return spectypes.BNRoleAttester, nil
	case "AGGREGATOR":
		return spectypes.BNRoleAggregator, nil
	case "PROPOSER":
		return spectypes.BNRoleProposer, nil
	case "SYNC_COMMITTEE":
		return spectypes.BNRoleSyncCommittee, nil
	case "SYNC_COMMITTEE_CONTRIBUTION":
		return spectypes.BNRoleSyncCommitteeContribution, nil
	case "VALIDATOR_REGISTRATION":
		return spectypes.BNRoleValidatorRegistration, nil
	case "VOLUNTARY_EXIT":
		return spectypes.BNRoleVoluntaryExit, nil
	default:
		return 0, fmt.Errorf("unknown role: %s", s)
	}
}

func RunnerRoleToString(r spectypes.RunnerRole) string {
	switch r {
	case spectypes.RoleCommittee:
		return "COMMITTEE"
	case spectypes.RoleAggregator:
		return "AGGREGATOR"
	case spectypes.RoleProposer:
		return "PROPOSER"
	case spectypes.RoleSyncCommitteeContribution:
		return "SYNC_COMMITTEE_CONTRIBUTION"
	case spectypes.RoleValidatorRegistration:
		return "VALIDATOR_REGISTRATION"
	case spectypes.RoleVoluntaryExit:
		return "VOLUNTARY_EXIT"
	default:
		return fmt.Sprintf("unknown(%d)", r)
	}
}

func PartialMsgTypeToString(mt spectypes.PartialSigMsgType) string {
	switch mt {
	case spectypes.PostConsensusPartialSig:
		return "PostConsensusPartialSig"
	case spectypes.RandaoPartialSig:
		return "RandaoPartialSig"
	case spectypes.SelectionProofPartialSig:
		return "SelectionProofPartialSig"
	case spectypes.ContributionProofs:
		return "ContributionProofs"
	case spectypes.ValidatorRegistrationPartialSig:
		return "ValidatorRegistrationPartialSig"
	case spectypes.VoluntaryExitPartialSig:
		return "VoluntaryExitPartialSig"
	default:
		return fmt.Sprintf("unknown(%d)", mt)
	}
}
