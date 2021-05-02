package collections

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/storage"
	"go.uber.org/zap"
)

// IbftStorage struct
type IbftStorage struct {
	prefix []byte
	db     storage.Db
	logger *zap.Logger
}

// NewIbft create new ibft storage
func NewIbft(db storage.Db, logger *zap.Logger) IbftStorage {
	ibft := IbftStorage{
		prefix: []byte("ibft"),
		db:     db,
		logger: logger,
	}
	return ibft
}

// SavePrepared func implementation
func (i *IbftStorage) SavePrepared(signedMsg *proto.SignedMessage) {

}

// SaveDecided func implementation
func (i *IbftStorage) SaveDecided(signedMsg *proto.SignedMessage) {

}
