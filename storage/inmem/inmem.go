package inmem

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/storage"
)

// inMemStorage implements storage.Storage interface
type inMemStorage struct {
}

// New is the constructor of inMemStorage
func New() storage.Storage {
	return &inMemStorage{}
}

func (s *inMemStorage) SavePrepared(signedMsg *proto.SignedMessage) {
	// TODO: Implement
}

func (s *inMemStorage) SaveDecided(signedMsg *proto.SignedMessage) {
	// TODO: Implement
}
