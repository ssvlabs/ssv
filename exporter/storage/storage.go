package storage

import (
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/storage/basedb"
	"go.uber.org/zap"
	"math"
)

var (
	storagePrefix = []byte("exporter/")
	syncOffsetKey = []byte("syncOffset")
)

// Storage represents the interface of exporter storage
type Storage interface {
	eth1.SyncOffsetStorage
	OperatorsCollection
	ValidatorsCollection
}

type exporterStorage struct {
	db     basedb.IDb
	logger *zap.Logger
}

// NewExporterStorage creates a new instance of Storage
func NewExporterStorage(db basedb.IDb, logger *zap.Logger) Storage {
	es := exporterStorage{db, logger.With(zap.String("component", "exporter/storage"))}
	return &es
}

// SaveSyncOffset saves the offset
func (es *exporterStorage) SaveSyncOffset(offset *eth1.SyncOffset) error {
	return es.db.Set(storagePrefix, syncOffsetKey, offset.Bytes())
}

// GetSyncOffset returns the offset
func (es *exporterStorage) GetSyncOffset() (*eth1.SyncOffset, error) {
	obj, err := es.db.Get(storagePrefix, syncOffsetKey)
	if err != nil {
		return nil, err
	}
	offset := new(eth1.SyncOffset)
	offset.SetBytes(obj.Value)
	return offset, nil
}

// nextIndex returns the next index for operator
func (es *exporterStorage) nextIndex(prefix []byte) (int64, error) {
	n, err := es.db.CountByCollection(append(storagePrefix, prefix...))
	if err != nil {
		return 0, err
	}
	return n, err
}

func normalTo(to int64) int64 {
	if to == 0 {
		return math.MaxInt64
	}
	return to
}
