package storage

import (
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/storage/basedb"
	"go.uber.org/zap"
	"math"
)

var (
	storagePrefix = []byte("exporter/")
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
