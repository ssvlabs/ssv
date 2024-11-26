package validation

import (
	"fmt"

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

func (ci *CommitteeInfo) signerIndex(signer spectypes.OperatorID) int {
	idx, ok := ci.signerIndices[signer]
	if !ok {
		panic(fmt.Sprintf("BUG: message validation must have checked that signer %v is in committee %v", signer, ci.committee))
	}

	return idx
}
