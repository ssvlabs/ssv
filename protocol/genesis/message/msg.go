package message

import (
	"fmt"

	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
)

const (
	// SSVSyncMsgType extends spec msg type
	SSVSyncMsgType genesisspectypes.MsgType = 100
	// SSVEventMsgType extends spec msg type
	SSVEventMsgType genesisspectypes.MsgType = 200
)

// MsgTypeToString extension for spec msg type. convert spec msg type to string
func MsgTypeToString(mt genesisspectypes.MsgType) string {
	switch mt {
	case genesisspectypes.SSVConsensusMsgType:
		return "consensus"
	case genesisspectypes.SSVPartialSignatureMsgType:
		return "partial_signature"
	case genesisspectypes.DKGMsgType:
		return "dkg"
	case SSVSyncMsgType:
		return "sync"
	case SSVEventMsgType:
		return "event"
	default:
		return fmt.Sprintf("unknown(%d)", mt)
	}
}

func QBFTMsgTypeToString(mt genesisspecqbft.MessageType) string {
	switch mt {
	case genesisspecqbft.ProposalMsgType:
		return "proposal"
	case genesisspecqbft.PrepareMsgType:
		return "prepare"
	case genesisspecqbft.CommitMsgType:
		return "commit"
	case genesisspecqbft.RoundChangeMsgType:
		return "round_change"
	default:
		return fmt.Sprintf("unknown(%d)", mt)
	}
}

// BeaconRoleFromString returns BeaconRole from string
func BeaconRoleFromString(s string) (genesisspectypes.BeaconRole, error) {
	switch s {
	case "ATTESTER":
		return genesisspectypes.BNRoleAttester, nil
	case "AGGREGATOR":
		return genesisspectypes.BNRoleAggregator, nil
	case "PROPOSER":
		return genesisspectypes.BNRoleProposer, nil
	case "SYNC_COMMITTEE":
		return genesisspectypes.BNRoleSyncCommittee, nil
	case "SYNC_COMMITTEE_CONTRIBUTION":
		return genesisspectypes.BNRoleSyncCommitteeContribution, nil
	case "VALIDATOR_REGISTRATION":
		return genesisspectypes.BNRoleValidatorRegistration, nil
	case "VOLUNTARY_EXIT":
		return genesisspectypes.BNRoleVoluntaryExit, nil
	default:
		return 0, fmt.Errorf("unknown role: %s", s)
	}
}
