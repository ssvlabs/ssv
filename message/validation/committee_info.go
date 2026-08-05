package validation

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

type CommitteeInfo struct {
	committeeID      spectypes.CommitteeID
	committee        []spectypes.OperatorID
	signerIndices    map[spectypes.OperatorID]int
	validatorIndices []phase0.ValidatorIndex
	// subnet is the Boole-fork committee subnet, precomputed once so the per-message
	// validation hot path (validateTopicAtSlot) never has to recompute a SHA-256-derived subnet.
	subnet uint64
	// subnetAlan is the pre-fork (Alan) committee subnet, precomputed for the same reason.
	subnetAlan uint64
}

// newCommitteeInfo requires the caller to supply both subnets explicitly (unlike the boole-fork
// reference's struct-literal constructor, which silently defaults forgotten fields to zero - a
// valid-looking-but-wrong subnet). Making them required parameters forces every call site to
// compute and pass them, so a missing subnet is a compile error rather than a runtime bug.
func newCommitteeInfo(
	committeeID spectypes.CommitteeID,
	operators []spectypes.OperatorID,
	validatorIndices []phase0.ValidatorIndex,
	booleSubnet uint64,
	alanSubnet uint64,
) CommitteeInfo {
	signerIndices := make(map[spectypes.OperatorID]int)
	for i, operator := range operators {
		signerIndices[operator] = i
	}

	return CommitteeInfo{
		committeeID:      committeeID,
		committee:        operators,
		signerIndices:    signerIndices,
		validatorIndices: validatorIndices,
		subnet:           booleSubnet,
		subnetAlan:       alanSubnet,
	}
}

// keeping the method for readability and the comment
func (ci *CommitteeInfo) signerIndex(signer spectypes.OperatorID) int {
	return ci.signerIndices[signer] // existence must be checked by ErrSignerNotInCommittee
}
