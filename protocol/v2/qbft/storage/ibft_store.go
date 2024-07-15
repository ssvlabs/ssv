package qbftstorage

import (
	"encoding/json"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/exporter/convert"
	"go.uber.org/zap"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
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

// InstanceStore manages instance data.
type InstanceStore interface {
	// GetHighestInstance returns the highest instance for the given identifier.
	GetHighestInstance(identifier []byte) (*StoredInstance, error)

	// GetInstancesInRange returns historical instances in the given range.
	GetInstancesInRange(identifier []byte, from specqbft.Height, to specqbft.Height) ([]*StoredInstance, error)

	// SaveInstance updates/inserts the given instance to it's identifier's history.
	SaveInstance(instance *StoredInstance) error

	// SaveHighestInstance saves the given instance as the highest of it's identifier.
	SaveHighestInstance(instance *StoredInstance) error

	// SaveHighestAndHistoricalInstance saves the given instance as both the highest and historical.
	SaveHighestAndHistoricalInstance(instance *StoredInstance) error

	// GetInstance returns an historical instance for the given identifier and height.
	GetInstance(identifier []byte, height specqbft.Height) (*StoredInstance, error)

	// CleanAllInstances removes all historical and highest instances for the given identifier.
	CleanAllInstances(logger *zap.Logger, msgID []byte) error

	// SaveParticipants save participants in quorum.
	SaveParticipants(identifier convert.MessageID, slot phase0.Slot, operators []spectypes.OperatorID) error

	// GetParticipantsInRange returns participants in quorum for the given slot range.
	GetParticipantsInRange(identifier convert.MessageID, from, to phase0.Slot) ([]ParticipantsRangeEntry, error)

	// GetParticipants returns participants in quorum for the given slot.
	GetParticipants(identifier convert.MessageID, slot phase0.Slot) ([]spectypes.OperatorID, error)
}

// QBFTStore is the store used by QBFT components
type QBFTStore interface {
	InstanceStore
}
