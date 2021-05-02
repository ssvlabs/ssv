package collections

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/storage"
	"go.uber.org/zap"
)

type Ibft struct {
	prefix []byte
	db     storage.Db
	logger *zap.Logger
}

func NewIbft(db storage.Db, logger *zap.Logger) Ibft {
	ibft := Ibft{
		prefix: []byte("ibft"),
		db:     db,
		logger: logger,
	}
	return ibft
}

func (i *Ibft) SavePrepared(signedMsg *proto.SignedMessage) {

}
func (i *Ibft) SaveDecided(signedMsg *proto.SignedMessage) {

}
