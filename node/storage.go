package node

import (
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/storage/basedb"
	"go.uber.org/zap"
	"math/big"
)

var (
	prefix        = []byte("ssvnode/")
	syncOffsetKey = []byte("syncOffset")
)

// Storage represents the interface for ssv node storage
type Storage interface {
	eth1.SyncOffsetStorage
}

type storage struct {
	db     basedb.IDb
	logger *zap.Logger
}

// NewSSVNodeStorage creates a new instance of Storage
func NewSSVNodeStorage(db basedb.IDb, logger *zap.Logger) Storage {
	es := storage{db, logger}
	return &es
}

// SaveSyncOffset saves the offset
func (s *storage) SaveSyncOffset(offset *eth1.SyncOffset) error {
	return s.db.Set(prefix, syncOffsetKey, offset.Bytes())
}

// GetSyncOffset returns the offset
func (s *storage) GetSyncOffset() (*eth1.SyncOffset, error) {
	obj, err := s.db.Get(prefix, syncOffsetKey)
	if err != nil {
		return nil, err
	}
	offset := new(big.Int)
	offset.SetBytes(obj.Value)
	return offset, nil
}
