package qbftstorage

import (
	"encoding/json"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// Participation extends ParticipantsRangeEntry with role and pubkey.
type Participation struct {
	ParticipantsRangeEntry
	Role   spectypes.BeaconRole
	PubKey spectypes.ValidatorPK
}

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
	Slot    phase0.Slot
	Signers []spectypes.OperatorID
}

// ParticipantStore is the store used by QBFT components
type ParticipantStore interface {
	// CleanAllInstances removes all historical and highest instances for the given identifier.
	CleanAllInstances(msgID []byte) error

	// UpdateParticipants updates participants in quorum.
	UpdateParticipants(role spectypes.BeaconRole, pk spectypes.ValidatorPK, slot phase0.Slot, newParticipants []spectypes.OperatorID) (bool, error)

	// GetParticipantsInRange returns participants in quorum for the given slot range.
	GetParticipantsInRange(role spectypes.BeaconRole, pk spectypes.ValidatorPK, from, to phase0.Slot) ([]ParticipantsRangeEntry, error)

	// GetParticipants returns participants in quorum for the given slot.
	GetParticipants(role spectypes.BeaconRole, pk spectypes.ValidatorPK, slot phase0.Slot) ([]spectypes.OperatorID, error)
}
