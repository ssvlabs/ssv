package message

import (
	"fmt"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

const (
	// SSVEventMsgType extends spec msg type
	SSVEventMsgType spectypes.MsgType = 200

	// SSVTBFTMsgType carries TBFT-protocol envelopes (Onion / NonReceipt /
	// Candidate, see protocol/v2/tbft/wire) inside a SignedSSVMessage.
	//
	// PLACEHOLDER VALUE: 0xF0 (= 240). Allocate a stable ecosystem-wide
	// value before mainnet — older SSV nodes that don't recognise this
	// type will reject the message via ErrUnknownSSVMessageType (a
	// libp2p-pubsub `Reject` outcome that drops the message and decrements
	// the sender's peer score). Mixed-cluster rollouts therefore degrade
	// gossip; rollout must be coordinated cluster-wide.
	SSVTBFTMsgType spectypes.MsgType = 0xF0

	// SSVDKGMsgType carries TBFT-IBE DKG ceremony envelopes (Exchange /
	// Deal / Response / Justification, see protocol/v2/dkg/wire) inside
	// a SignedSSVMessage. Used once-per-cluster-lifetime to establish
	// the IBE keypair under Option B (see docs/TBFT-DKG-TASKS.md).
	//
	// PLACEHOLDER VALUE: 0xF1 (= 241). Same mainnet-allocation caveat as
	// SSVTBFTMsgType.
	SSVDKGMsgType spectypes.MsgType = 0xF1
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
	case SSVTBFTMsgType:
		return "tbft"
	case SSVDKGMsgType:
		return "dkg"
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
