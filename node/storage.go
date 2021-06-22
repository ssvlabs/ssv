package node

import (
	"github.com/bloxapp/ssv/storage/basedb"
	"go.uber.org/zap"
	"math/big"
)

var (
	prefix        = []byte("ssvnode/")
	syncOffsetKey = []byte("syncOffset")
)

// SyncOffset is the type of variable used for passing around the offset
type SyncOffset = big.Int

// Storage represents the interface for ssv node storage
type Storage interface {
	// SaveSyncOffset saves the offset (block number)
	SaveSyncOffset(offset *SyncOffset) error
	// GetSyncOffset returns the sync offset
	GetSyncOffset() (*SyncOffset, error)
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
func (s *storage) SaveSyncOffset(offset *SyncOffset) error {
	return s.db.Set(prefix, syncOffsetKey, offset.Bytes())
}

// GetSyncOffset returns the offset
func (s *storage) GetSyncOffset() (*SyncOffset, error) {
	obj, err := s.db.Get(prefix, syncOffsetKey)
	if err != nil {
		return nil, err
	}
	offset := new(big.Int)
	offset.SetBytes(obj.Value)
	return offset, nil
}
