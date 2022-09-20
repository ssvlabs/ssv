package migrations

import (
	"context"
)

var migrationCleanSyncOffset = Migration{
	Name: "migration_6_clean_sync_offset",
	Run: func(ctx context.Context, opt Options, key []byte) error {
		nodeStorage := opt.nodeStorage()
		if err := nodeStorage.CleanRegistryData(); err != nil {
			return err
		}
		return opt.Db.Set(migrationsPrefix, key, migrationCompleted)
	},
}
