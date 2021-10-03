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

// IDb interface for all db kind
type IDb interface {
	Set(prefix []byte, key []byte, value []byte) error
	Get(prefix []byte, key []byte) (Obj, bool, error)
	GetAllByCollection(prefix []byte) ([]Obj, error)
	CountByCollection(prefix []byte) (int64, error)
	RemoveAllByCollection(prefix []byte) error
	Close()
}

// Obj struct for getting key/value from storage
type Obj struct {
	Key   []byte
	Value []byte
}
