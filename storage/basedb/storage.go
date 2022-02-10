package basedb

import (
	"context"

	"go.uber.org/zap"
)

// Options for creating all db type
type Options struct {
	Type      string `yaml:"Type" env:"DB_TYPE" env-default:"badger-db" env-description:"Type of db badger-db or badger-memory"`
	Path      string `yaml:"Path" env:"DB_PATH" env-default:"./data/db" env-description:"Path for storage"`
	Reporting bool   `yaml:"Reporting" env:"DB_REPORTING" env-default:"false" env-description:"Flag to run on-off db size reporting"`
	Logger    *zap.Logger
	Ctx       context.Context
}

// Txn interface for badger transaction like functions
type Txn interface {
	Set(prefix []byte, key []byte, value []byte) error
	Get(prefix []byte, key []byte) (Obj, bool, error)
	Delete(prefix []byte, key []byte) error
	// TODO: add iterator
}

// RegistryStore interface for registry store
// TODO: extend this interface and re-think storage refactoring
type RegistryStore interface {
	CleanRegistryData() error
}

// IDb interface for all db kind
type IDb interface {
	Set(prefix []byte, key []byte, value []byte) error
	SetMany(prefix []byte, n int, next func(int) (Obj, error)) error
	Get(prefix []byte, key []byte) (Obj, bool, error)
	GetMany(prefix []byte, keys [][]byte, iterator func(Obj) error) error
	Delete(prefix []byte, key []byte) error
	GetAll(prefix []byte, handler func(int, Obj) error) error
	CountByCollection(prefix []byte) (int64, error)
	RemoveAllByCollection(prefix []byte) error
	Update(fn func(Txn) error) error
	Close()
}

// Obj struct for getting key/value from storage
type Obj struct {
	Key   []byte
	Value []byte
}
