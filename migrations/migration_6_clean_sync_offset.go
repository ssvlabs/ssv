package migrations

import (
	"context"
	"go.uber.org/zap"
)

var migrationCleanSyncOffset = Migration{
	Name: "migration_6_clean_sync_offset",
	Run: func(ctx context.Context, logger *zap.Logger, opt Options, key []byte) error {
		nodeStorage := opt.nodeStorage(logger)
		if err := nodeStorage.CleanRegistryData(); err != nil {
			return err
		}
		return opt.Db.Set(migrationsPrefix, key, migrationCompleted)
	},
}
