package validation

import (
	"fmt"
	"math/bits"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// TODO: Take all of these from https://github.com/ssvlabs/ssv/pull/1867 once it's merged.
// This file is temporary to avoid the need to be based on another PR, hence there are no tests.

const maxCommitteeSize = 13

type Quorum struct {
	Signers   []spectypes.OperatorID
	Committee []spectypes.OperatorID
}

func (q *Quorum) ToBitMask() SignersBitMask {
	if len(q.Signers) > maxCommitteeSize || len(q.Committee) > maxCommitteeSize || len(q.Signers) > len(q.Committee) {
		panic(fmt.Sprintf("invalid signers/committee size: %d/%d", len(q.Signers), len(q.Committee)))
	}

	bitmask := SignersBitMask(0)
	i, j := 0, 0
	for i < len(q.Signers) && j < len(q.Committee) {
		if q.Signers[i] == q.Committee[j] {
			bitmask |= 1 << uint(j) // #nosec G115 -- j cannot exceed maxCommitteeSize
			i++
			j++
		} else if q.Signers[i] < q.Committee[j] {
			i++
		} else { // A[i] > B[j]
			j++
		}
	}

	return bitmask
}

type SignersBitMask uint16

func (obm SignersBitMask) SignersList(committee []spectypes.OperatorID) []spectypes.OperatorID {
	if len(committee) > maxCommitteeSize {
		panic(fmt.Sprintf("invalid committee size: %d", len(committee)))
	}

	signers := make([]spectypes.OperatorID, 0, bits.OnesCount16(uint16(obm)))
	for j := 0; j < len(committee); j++ {
		// #nosec G115 -- j cannot exceed maxCommitteeSize
		if obm&(1<<uint(j)) != 0 {
			signers = append(signers, committee[j])
		}
	}

	return signers
}
