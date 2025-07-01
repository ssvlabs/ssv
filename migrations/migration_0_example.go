package migrations

import (
	"context"

	"go.uber.org/zap"
)

// This migration is an Example migration
var migration_0_example = Migration{
	Name: "migration_0_example",
	Run: func(ctx context.Context, logger *zap.Logger, opt Options, key []byte, completed CompletedFunc) error {
		type dbObject struct {
			key   []byte
			value []byte
		}

		// Example to clean registry data for specific storage
		nodeStorage, err := opt.nodeStorage(logger)
		if err != nil {
			return err
		}
		if err := nodeStorage.DropRegistryData(); err != nil {
			return err
		}

		// Using SetMany, the following 3 updates either all happen and are committed,
		// or they all rollback.
		// If we used Set 3 times independently, the migration could abort in the middle
		// with only some of the Set's committed, resulting in a corrupted database.
		sets := []dbObject{
			{
				key:   []byte("owner-address"),
				value: []byte("123"),
			},
			{
				key:   []byte("operators-public-keys"),
				value: []byte("abc"),
			},
			{
				key:   key,
				value: migrationCompleted,
			},
		}
		return opt.Db.SetMany(migrationsPrefix, 3, func(i int) (key, value []byte, err error) {
			return sets[i].key, sets[i].value, nil
		})
	},
}
