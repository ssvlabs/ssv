package storage

import (
	"github.com/bloxapp/ssv/storage/basedb"
	"go.uber.org/zap"
	"math/big"
)

var (
	prefix        = []byte("exporter/")
	syncOffsetKey = []byte("syncOffset")
)

// ExporterStorage represents the interface of the relevant storage
type ExporterStorage interface {
	// SaveSyncOffset saves the offset (block number)
	SaveSyncOffset(offset *big.Int) error
	// GetSyncOffset returns the sync offset
	GetSyncOffset() (*big.Int, error)
}

type exporterStorage struct {
	db     basedb.IDb
	logger *zap.Logger
}

// NewExporterStorage creates a new instance of ExporterStorage
func NewExporterStorage(db basedb.IDb, logger *zap.Logger) ExporterStorage {
	es := exporterStorage{db, logger}
	return &es
}

// SaveSyncOffset saves the offset
func (es *exporterStorage) SaveSyncOffset(offset *big.Int) error {
	return es.db.Set(prefix, syncOffsetKey, offset.Bytes())
}

// GetSyncOffset returns the offset
func (es *exporterStorage) GetSyncOffset() (*big.Int, error) {
	obj, err := es.db.Get(prefix, syncOffsetKey)
	if err != nil {
		return nil, err
	}
	offset := new(big.Int)
	offset.SetBytes(obj.Value)
	return offset, nil
}
