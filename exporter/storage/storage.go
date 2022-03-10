package storage

import (
	"math"
	"sync"

	"github.com/bloxapp/ssv/eth1"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"go.uber.org/zap"
)

func storagePrefix() []byte {
	return []byte("exporter/")
}

// Storage represents the interface of exporter storage
type Storage interface {
	eth1.SyncOffsetStorage
	registrystorage.OperatorsCollection
	ValidatorsCollection
	basedb.RegistryStore
}

type storage struct {
	db     basedb.IDb
	logger *zap.Logger

	validatorsLock sync.RWMutex

	operatorStore registrystorage.OperatorsCollection
}

func (s *storage) GetOperatorInformation(operatorPubKey string) (*registrystorage.OperatorInformation, bool, error) {
	return s.operatorStore.GetOperatorInformation(operatorPubKey)
}

func (s *storage) SaveOperatorInformation(operatorInformation *registrystorage.OperatorInformation) error {
	return s.operatorStore.SaveOperatorInformation(operatorInformation)
}

func (s *storage) ListOperators(from int64, to int64) ([]registrystorage.OperatorInformation, error) {
	return s.operatorStore.ListOperators(from, to)
}

// NewExporterStorage creates a new instance of Storage
func NewExporterStorage(db basedb.IDb, logger *zap.Logger) Storage {
	return &storage{
		db:            db,
		logger:        logger.With(zap.String("component", "exporter/storage")),
		operatorStore: registrystorage.NewOperatorsStorage(db, logger, storagePrefix()),
	}
}

// CleanRegistryData clears storage registry data
func (s *storage) CleanRegistryData() error {
	return s.db.RemoveAllByCollection(storagePrefix())
}

// nextIndex returns the next index for operator
func (s *storage) nextIndex(prefix []byte) (int64, error) {
	n, err := s.db.CountByCollection(append(storagePrefix(), prefix...))
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
