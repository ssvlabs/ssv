package inmem

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/storage/collections"
)

// inMemStorage implements storage.Storage interface
type inMemStorage struct {
}

// New is the constructor of inMemStorage
func New() collections.Iibft {
	return &inMemStorage{}
}

// SaveCurrentInstance saves the state for the current running (not yet decided) instance
func (s *inMemStorage) SaveCurrentInstance(state *proto.State) error {
	return nil
}

// GetCurrentInstance returns the state for the current running (not yet decided) instance
func (s *inMemStorage) GetCurrentInstance(pk []byte) (*proto.State, error) {
	return nil, nil
}

// SaveDecided saves a signed message for an ibft instance with decided justification
func (s *inMemStorage) SaveDecided(signedMsg *proto.SignedMessage) error {
	return nil
}

// GetDecided returns a signed message for an ibft instance which decided by identifier
func (s *inMemStorage) GetDecided(pk []byte, seqNumber uint64) (*proto.SignedMessage, error) {
	return nil, nil
}

// SaveHighestDecidedInstance saves a signed message for an ibft instance which is currently highest
func (s *inMemStorage) SaveHighestDecidedInstance(signedMsg *proto.SignedMessage) error {
	return nil
}

// GetHighestDecidedInstance gets a signed message for an ibft instance which is the highest
func (s *inMemStorage) GetHighestDecidedInstance(pk []byte) (*proto.SignedMessage, error) {
	return nil, nil
}
