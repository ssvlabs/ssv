package storage

import (
	"fmt"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
)

func GetStorageFactory(options basedb.Options) (basedb.IDb, error) {
	switch options.Type {
	case "db":
		db, err :=kv.New(options)
		return db, err
	case "memory":
		return nil, fmt.Errorf("not implemented storage type")
	}
	return nil, fmt.Errorf("unsupported storage type passed")
}