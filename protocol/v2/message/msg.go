package message

import (
	"fmt"
	spectypes "github.com/bloxapp/ssv-spec/types"
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
		return "partialSignature"
	case spectypes.DKGMsgType:
		return "dkg"
	case SSVSyncMsgType:
		return "sync"
	case SSVEventMsgType:
		return "event"
	default:
		return fmt.Sprintf("unknown - %d", mt)
	}
}

// BeaconRoleFromString returns BeaconRole from string
func BeaconRoleFromString(s string) spectypes.BeaconRole {
	switch s {
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
		// TODO(nkryuchkov): don't panic
		panic("unknown role")
	}
}
