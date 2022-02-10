package migrations

import (
	"context"

	"github.com/bloxapp/ssv/storage/basedb"
)

// This migration is an Example migration
var migrationExample1 = Migration{
	Name: "migration_0_example",
	Run: func(ctx context.Context, opt Options, key []byte) error {
		// Example to clean registry data for specific storage
		if err := opt.exporterStorage().CleanRegistryData(); err != nil {
			return err
		}

		// Using SetMany, the following 3 updates either all happen and are committed,
		// or they all rollback.
		// If we used Set 3 times independently, the migration could abort in the middle
		// with only some of the Set's committed, resulting in a corrupted database.
		sets := []basedb.Obj{
			{
				Key:   []byte("owner-address"),
				Value: []byte("123"),
			},
			{
				Key:   []byte("operators-public-keys"),
				Value: []byte("abc"),
			},
			{
				Key:   key,
				Value: migrationCompleted,
			},
		}
		return opt.Db.SetMany(migrationsPrefix, 3, func(i int) (basedb.Obj, error) {
			return sets[i], nil
		})
	},
}
