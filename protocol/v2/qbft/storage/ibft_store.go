package qbftstorage

import (
	"encoding/json"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
)

// StoredInstance contains instance state alongside with a decided message (aggregated commits).
type StoredInstance struct {
	State          *specqbft.State
	DecidedMessage *specqbft.SignedMessage
}

// Encode returns a StoredInstance encoded bytes or error.
func (si *StoredInstance) Encode() ([]byte, error) {
	return json.Marshal(si)
}

// Decode returns error if decoding failed.
func (si *StoredInstance) Decode(data []byte) error {
	return json.Unmarshal(data, &si)
}

// InstanceStore manages instance data.
type InstanceStore interface {
	// SaveHighestInstance saves the StoredInstance for the highest instance.
	SaveHighestInstance(instance *StoredInstance) error
	// GetHighestInstance returns the StoredInstance for the highest instance.
	GetHighestInstance(identifier []byte) (*StoredInstance, error)
	// GetInstancesInRange returns historical StoredInstance's in the given range.
	GetInstancesInRange(identifier []byte, from specqbft.Height, to specqbft.Height) ([]*StoredInstance, error)
	// SaveInstance saves historical StoredInstance.
	SaveInstance(instance *StoredInstance) error
	// CleanAllInstances removes all StoredInstance's & highest StoredInstance's for msgID.
	CleanAllInstances(msgID []byte) error
}

// ChangeRoundStore manages change round data
type ChangeRoundStore interface {
	// GetLastChangeRoundMsg returns the latest broadcasted msg from the instance
	GetLastChangeRoundMsg(identifier []byte, signers ...spectypes.OperatorID) ([]*specqbft.SignedMessage, error)
	// SaveLastChangeRoundMsg returns the latest broadcasted msg from the instance
	SaveLastChangeRoundMsg(msg *specqbft.SignedMessage) error
	// CleanLastChangeRound cleans last change round message of some validator, should be called upon controller init
	CleanLastChangeRound(identifier []byte) error
}

// QBFTStore is the store used by QBFT components
type QBFTStore interface {
	InstanceStore
	ChangeRoundStore
}
