package qbftstorage

import (
	"context"
	"encoding/json"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/operator/slotticker"
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
	PubKey  spectypes.ValidatorPK
	Signers []spectypes.OperatorID
}

// ParticipantStore is the store used by QBFT components
type ParticipantStore interface {
	// CleanAllInstances removes all records in old format.
	CleanAllInstances() error

	// UpdateParticipants updates participants in quorum.
	UpdateParticipants(pk spectypes.ValidatorPK, slot phase0.Slot, newParticipants []spectypes.OperatorID) (bool, error)

	// GetParticipantsInRange returns participants in quorum for the given slot range.
	GetAllParticipantsInRange(from, to phase0.Slot) ([]ParticipantsRangeEntry, error)

	// GetParticipantsInRange returns participants in quorum for the given slot range and validator public key.
	GetParticipantsInRange(pk spectypes.ValidatorPK, from, to phase0.Slot) ([]ParticipantsRangeEntry, error)

	// GetParticipants returns participants in quorum for the given slot.
	GetParticipants(pk spectypes.ValidatorPK, slot phase0.Slot) ([]spectypes.OperatorID, error)

	// StartCleanupJob performs an initial cleanup (blocking) then starts a background job to
	// make sure that only a given number of slots will be kept back in the db
	StartCleanupJob(ctx context.Context, logger *zap.Logger, slotTickerProvider slotticker.Provider, retain int)
}
