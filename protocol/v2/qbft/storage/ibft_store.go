package qbftstorage

import (
	"encoding/json"
	"fmt"
	"math/bits"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/exporter/convert"
)

const maxCommitteeSize = 13 // TODO: define in ssv network config

// StoredInstance contains instance state alongside with a decided message (aggregated commits).
type StoredInstance struct {
	State          *specqbft.State
	DecidedMessage *spectypes.SignedSSVMessage
}

// Encode returns a StoredInstance encoded bytes or error.
func (si *StoredInstance) Encode() ([]byte, error) {
	return json.Marshal(si)
}

// Decode returns error if decoding failed.
func (si *StoredInstance) Decode(data []byte) error {
	return json.Unmarshal(data, &si)
}

type ParticipantsRangeEntry struct {
	Slot       phase0.Slot
	Signers    []spectypes.OperatorID
	Identifier convert.MessageID
}

type Quorum struct {
	Signers     []spectypes.OperatorID
	Committee   []spectypes.OperatorID
	ValidatorPK spectypes.ValidatorPK // optional payload
}

func (q *Quorum) ToSignersBitMask() SignersBitMask {
	if len(q.Committee) > maxCommitteeSize || len(q.Signers) > maxCommitteeSize || len(q.Signers) > len(q.Committee) {
		panic(fmt.Sprintf("invalid signers/quorum size: %d/%d", len(q.Committee), len(q.Signers)))
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

// SignersBitMask represents a bitset of committee indices of operators participated in the quorum.
// As the maximal supported operator count is 13, it needs to be at least 13 bits long.
// The starting bit is the least significant bit, so it represents the first operator in the committee.
//
// Example:
// If committee is [1,2,3,4] and SignersBitMask is 0b0000_0000_0000_1101, it means quorum of [1,3,4].
type SignersBitMask uint16

func (obm SignersBitMask) Signers(committee []spectypes.OperatorID) []spectypes.OperatorID {
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

// QBFTStore is the store used by QBFT components
type QBFTStore interface {
	// CleanAllInstances removes all historical and highest instances for the given identifier.
	CleanAllInstances(msgID []byte) error

	// UpdateParticipants updates participants in quorum.
	UpdateParticipants(identifier convert.MessageID, slot phase0.Slot, newParticipants Quorum) (bool, error)

	// GetParticipantsInRange returns participants in quorum for the given slot range.
	GetParticipantsInRange(identifier convert.MessageID, from, to phase0.Slot, committee []spectypes.OperatorID) ([]ParticipantsRangeEntry, error)

	// GetParticipants returns participants in quorum for the given slot.
	GetParticipants(identifier convert.MessageID, slot phase0.Slot, committee []spectypes.OperatorID) ([]spectypes.OperatorID, error)
}
