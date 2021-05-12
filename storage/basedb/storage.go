package basedb

import "go.uber.org/zap"

type Options struct {
	Type   string `yaml:"Type" env:"DB_TYPE" env-default:"db" env-description:"Type of db storage or memory"`
	Path   string `yaml:"Path" env:"DB_PATH" env-default:"./data/db" env-description:"Path for storage"`
	Logger *zap.Logger
}

// Db interface for all db kind
type IDb interface {
	Set(prefix []byte, key []byte, value []byte) error
	Get(prefix []byte, key []byte) (Obj, error)
	GetAllByBucket(prefix []byte) ([]Obj, error)
}

// Obj struct for getting key/value from storage
type Obj struct {
	Key   []byte
	Value []byte
}