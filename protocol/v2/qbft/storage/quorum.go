package qbftstorage

import (
	"fmt"
	"math/bits"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// We currently don't support committee sizes more than 13, but this package may work with any sizes that fit uint16.
const uint16Bits = 16

type Quorum struct {
	Signers   []spectypes.OperatorID
	Committee []spectypes.OperatorID
}

func NewQuorum(signers, committee []spectypes.OperatorID) (Quorum, error) {
	q := Quorum{
		Signers:   signers,
		Committee: committee,
	}

	return q, q.validateOperators()
}

func (q *Quorum) ToSignersBitMask() SignersBitMask {
	if err := q.validateOperators(); err != nil {
		panic(err)
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
		} else { // q.Signers[i] > q.Committee[j]
			j++
		}
	}

	return bitmask
}

func (q *Quorum) validateOperators() error {
	if len(q.Signers) > uint16Bits || len(q.Committee) > uint16Bits || len(q.Signers) > len(q.Committee) {
		return fmt.Errorf("invalid signers/committee size: %d/%d", len(q.Signers), len(q.Committee))
	}

	committeeSet := make(map[spectypes.OperatorID]struct{})
	for _, id := range q.Committee {
		committeeSet[id] = struct{}{}
	}

	for _, id := range q.Signers {
		if _, ok := committeeSet[id]; !ok {
			return fmt.Errorf("signer %d is not in committee", id)
		}
	}

	return nil
}

// SignersBitMask represents a bitset of committee indices of operators participated in the quorum.
// As the maximal supported operator count is 13, it needs to be at least 13 bits long.
// The starting bit is the least significant bit, so it represents the first operator in the committee.
//
// Example:
// If committee is [1,2,3,4] and SignersBitMask is 0b0000_0000_0000_1101, it means quorum of [1,3,4].
type SignersBitMask uint16

func (obm SignersBitMask) Signers(committee []spectypes.OperatorID) ([]spectypes.OperatorID, error) {
	if len(committee) > uint16Bits {
		return nil, fmt.Errorf("unsupported committee size: %d", len(committee))
	}

	signers := make([]spectypes.OperatorID, 0, bits.OnesCount16(uint16(obm)))
	for j := 0; j < len(committee); j++ {
		// #nosec G115 -- j cannot exceed maxCommitteeSize
		if obm&(1<<uint(j)) != 0 {
			signers = append(signers, committee[j])
		}
	}

	return signers, nil
}
