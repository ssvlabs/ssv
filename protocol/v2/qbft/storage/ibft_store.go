package qbftstorage

import (
	"encoding/json"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/exporter/convert"
)

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

// QBFTStore is the store used by QBFT components
type QBFTStore interface {
	// CleanAllInstances removes all historical and highest instances for the given identifier.
	CleanAllInstances(msgID []byte) error

	// UpdateParticipants updates participants in quorum.
	UpdateParticipants(identifier convert.MessageID, slot phase0.Slot, newParticipants []spectypes.OperatorID) (bool, error)

	// GetParticipantsInRange returns participants in quorum for the given slot range.
	GetParticipantsInRange(identifier convert.MessageID, from, to phase0.Slot) ([]ParticipantsRangeEntry, error)

	// GetParticipantsInSlot returns participants in quorum for the given slot range.
	GetParticipantsInSlot(from, to phase0.Slot) ([]ParticipantsRangeEntry, error)
}
