package beacon

import (
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/protocol/v1/message"
)

// Duty represent data regarding the duty type with the duty data
type Duty struct {
	// Type is the duty type (attest, propose)
	Type message.RoleType
	// PubKey is the public key of the validator that should attest.
	PubKey spec.BLSPubKey
	// Slot is the slot in which the validator should attest.
	Slot spec.Slot
	// ValidatorIndex is the index of the validator that should attest.
	ValidatorIndex spec.ValidatorIndex
	// CommitteeIndex is the index of the committee in which the attesting validator has been placed.
	CommitteeIndex spec.CommitteeIndex
	// CommitteeLength is the length of the committee in which the attesting validator has been placed.
	CommitteeLength uint64
	// CommitteesAtSlot is the number of committees in the slot.
	CommitteesAtSlot uint64
	// ValidatorCommitteeIndex is the index of the validator in the list of validators in the committee.
	ValidatorCommitteeIndex uint64
}
