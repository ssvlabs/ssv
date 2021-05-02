package collections

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/storage"
	"go.uber.org/zap"
)

// Ibft struct
type Ibft struct {
	prefix []byte
	db     storage.Db
	logger *zap.Logger
}

// NewIbft create new ibft storage
func NewIbft(db storage.Db, logger *zap.Logger) Ibft {
	ibft := Ibft{
		prefix: []byte("ibft"),
		db:     db,
		logger: logger,
	}
	return ibft
}

// SavePrepared func implementation
func (i *Ibft) SavePrepared(signedMsg *proto.SignedMessage) {

}

// SaveDecided func implementation
func (i *Ibft) SaveDecided(signedMsg *proto.SignedMessage) {

}
