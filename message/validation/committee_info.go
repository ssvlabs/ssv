package validation

import (
	"sort"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

type CommitteeInfo struct {
	committeeID      spectypes.CommitteeID
	committee        []spectypes.OperatorID
	signerIndices    map[spectypes.OperatorID]int
	validatorIndices []phase0.ValidatorIndex
}

func newCommitteeInfo(
	committeeID spectypes.CommitteeID,
	operators []spectypes.OperatorID,
	validatorIndices []phase0.ValidatorIndex,
) CommitteeInfo {
	// Ensure deterministic order: sort operator IDs so signerIndex mapping is stable across calls.
	sort.Slice(operators, func(i, j int) bool { return operators[i] < operators[j] })

	signerIndices := make(map[spectypes.OperatorID]int)
	for i, operator := range operators {
		signerIndices[operator] = i
	}

	return CommitteeInfo{
		committeeID:      committeeID,
		committee:        operators,
		signerIndices:    signerIndices,
		validatorIndices: validatorIndices,
	}
}

// keeping the method for readability and the comment
func (ci *CommitteeInfo) signerIndex(signer spectypes.OperatorID) int {
	return ci.signerIndices[signer] // existence must be checked by ErrSignerNotInCommittee
}
