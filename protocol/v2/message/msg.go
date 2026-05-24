package message

import (
	"fmt"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

const (
	// SSVEventMsgType extends spec msg type
	SSVEventMsgType spectypes.MsgType = 200

	// SSVOBFTMsgType carries OBFT-protocol envelopes (Phase1Bundle / Onion /
	// NR / Certificate, see protocol/v2/obft/wire) inside a SignedSSVMessage.
	//
	// PLACEHOLDER VALUE: 0xF0 (= 240). Allocate a stable ecosystem-wide
	// value before mainnet — older SSV nodes that don't recognise this
	// type will reject the message via ErrUnknownSSVMessageType (a
	// libp2p-pubsub `Reject` outcome that drops the message and decrements
	// the sender's peer score). Mixed-cluster rollouts therefore degrade
	// gossip; rollout must be coordinated cluster-wide.
	SSVOBFTMsgType spectypes.MsgType = 0xF0

	// SSVDKGMsgType carries OBFT-IBE DKG ceremony envelopes (Exchange /
	// Deal / Response / Justification, see protocol/v2/dkg/wire) inside
	// a SignedSSVMessage. Used once-per-cluster-lifetime to establish
	// the IBE keypair under Option B.
	//
	// PLACEHOLDER VALUE: 0xF1 (= 241). Same mainnet-allocation caveat as
	// SSVOBFTMsgType.
	SSVDKGMsgType spectypes.MsgType = 0xF1

	// SSV2abOBFTMsgType carries 2abOBFT-protocol envelopes (Phase1Bundle /
	// Value / NoValue / Commit / Certificate, see protocol/v2/obft/twoab/wire)
	// inside a SignedSSVMessage. Distinct from SSVOBFTMsgType so the network
	// routes the two consensus variants to their respective dispatch paths;
	// the inner wire format additionally carries a distinct ProtocolTag, so a
	// mis-delivered envelope is rejected at decode regardless.
	//
	// PLACEHOLDER VALUE: 0xF2 (= 242). Same mainnet-allocation caveat as
	// SSVOBFTMsgType.
	SSV2abOBFTMsgType spectypes.MsgType = 0xF2
)

// RoleDKG is the RunnerRole used in DKG-ceremony MsgIDs. The dutyExecutorID
// slot of the MsgID carries the 32-byte clusterID (left-padded), mirroring
// the existing committee-MsgID pattern at
// protocol/v2/ssv/validator/committee.go: NewMsgID(domain, CommitteeID[:],
// RoleCommittee). Operators are already subscribed to the committee
// subnet for committee duties, so DKG envelopes naturally land where
// every cluster operator is listening.
//
// PLACEHOLDER VALUE: 0xF0 (= 240). Out of the spec's 0..5 range; allocate
// a stable ecosystem-wide value before mainnet alongside SSVDKGMsgType.
const RoleDKG spectypes.RunnerRole = 0xF0

// MsgTypeToString extension for spec msg type. convert spec msg type to string
func MsgTypeToString(mt spectypes.MsgType) string {
	switch mt {
	case spectypes.SSVConsensusMsgType:
		return "consensus"
	case spectypes.SSVPartialSignatureMsgType:
		return "partial_signature"
	case SSVEventMsgType:
		return "event"
	case SSVOBFTMsgType:
		return "obft"
	case SSV2abOBFTMsgType:
		return "2abobft"
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
