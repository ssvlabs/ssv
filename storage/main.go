package storage

import (
	"fmt"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
)

// GetStorageFactory resolve and returns db instance based on db type
func GetStorageFactory(logger *zap.Logger, options basedb.Options) (*kv.BadgerDb, error) {
	switch options.Type {
	case "badger-db":
		db, err := kv.New(logger, options)
		return db, err
	case "badger-memory":
		db, err := kv.New(logger, options)
		return db, err
	}
	return nil, fmt.Errorf("unsupported storage type passed")
}
