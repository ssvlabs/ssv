package migrations

import (
	"context"
	"fmt"

	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/pkg/errors"
)

// This migration is an Example of atomic
// View/Update transactions usage
var migrationExample2 = Migration{
	Name: "migration_1_example",
	Run: func(ctx context.Context, opt Options, key []byte) error {
		return opt.Db.Update(func(txn basedb.Txn) error {
			var (
				testPrefix = []byte("test_prefix/")
				testKey    = []byte("test_key")
				testValue  = []byte("test_value")
			)
			err := txn.Set(testPrefix, testKey, testValue)
			if err != nil {
				return err
			}
			obj, found, err := txn.Get(testPrefix, testKey)
			if err != nil {
				return err
			}
			if !found {
				return errors.Errorf("the key %s is not found", string(obj.Key))
			}
			fmt.Printf("the key %s is found. value = %s", string(obj.Key), string(obj.Key))
			return txn.Set(migrationsPrefix, key, migrationCompleted)
		})
	},
}
