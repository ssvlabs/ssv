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

func (s *inMemStorage) SavePrepareJustification(lambda []byte, round uint64, msg *proto.Message, signature []byte, signers []uint64) {
	// TODO: Implement
}

func (s *inMemStorage) SaveDecidedRound(lambda []byte, msg *proto.Message, signature []byte, signers []uint64) {
	// TODO: Implement
}
