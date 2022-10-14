package migrations

import (
	"context"
)

var migrationCleanOperatorRemovalCorruptions = Migration{
	Name: "migration_7_clean_operator_removal_corruptions",
	Run: func(ctx context.Context, opt Options, key []byte) error {
		nodeStorage := opt.nodeStorage()
		if err := nodeStorage.CleanRegistryData(); err != nil {
			return err
		}
		return opt.Db.Set(migrationsPrefix, key, migrationCompleted)
	},
}
