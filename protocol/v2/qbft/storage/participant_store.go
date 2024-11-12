package qbftstorage

import (
	"encoding/binary"
	"encoding/json"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

const (
	domainSize       = 4
	domainStartPos   = 0
	pubKeySize       = 48
	pubKeyStartPos   = domainStartPos + domainSize
	roleTypeSize     = 4
	roleTypeStartPos = pubKeyStartPos + pubKeySize
)

func legacyNewMsgID(domain spectypes.DomainType, pk []byte, role spectypes.BeaconRole) (mid spectypes.MessageID) {
	roleByts := make([]byte, 4)
	binary.LittleEndian.PutUint32(roleByts, uint32(role)) // nolint: gosec
	copy(mid[domainStartPos:domainStartPos+domainSize], domain[:])
	copy(mid[pubKeyStartPos:pubKeyStartPos+pubKeySize], pk)
	copy(mid[roleTypeStartPos:roleTypeStartPos+roleTypeSize], roleByts)
	return mid
}

type Participation struct {
	ParticipantsRangeEntry

	DomainType spectypes.DomainType
	Role       spectypes.BeaconRole
	PK         spectypes.ValidatorPK
}

// LegacyMsgID is needed only for API backwards-compatibility
func (p *Participation) LegacyMsgID() spectypes.MessageID {
	return legacyNewMsgID(p.DomainType, p.PK[:], p.Role)
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
