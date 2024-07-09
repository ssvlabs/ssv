package instance

import (
	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

func CheckSignersInCommittee(signedMsg *genesisspecqbft.SignedMessage, committee []*spectypes.ShareMember) bool {
	// Committee's operators map
	committeeMap := make(map[uint64]struct{})
	for _, operator := range committee {
		committeeMap[operator.Signer] = struct{}{}
	}

	// Check that all message signers belong to the map
	for _, signer := range signedMsg.Signers {
		if _, ok := committeeMap[signer]; !ok {
			return false
		}
	}
	return true
}

func HasQuorum(share *spectypes.CommitteeMember, msgs []*genesisspecqbft.SignedMessage) bool {
	uniqueSigners := make(map[spectypes.OperatorID]bool)
	for _, msg := range msgs {
		for _, signer := range msg.GetSigners() {
			uniqueSigners[signer] = true
		}
	}
	return share.HasQuorum(len(uniqueSigners))
}

// HasPartialQuorum returns true if a unique set of signers has partial quorum
func HasPartialQuorum(share *spectypes.CommitteeMember, msgs []*genesisspecqbft.SignedMessage) bool {
	uniqueSigners := make(map[spectypes.OperatorID]bool)
	for _, msg := range msgs {
		for _, signer := range msg.GetSigners() {
			uniqueSigners[signer] = true
		}
	}

	_, pc := ssvtypes.ComputeQuorumAndPartialQuorum(len(share.Committee))
	return uint64(len(uniqueSigners)) >= pc
}
