package migrations

import (
	"context"

	"go.uber.org/zap"
)

var migrationCleanRegistryData = Migration{
	Name: "migration_11_clean_all_registry_data",
	Run: func(ctx context.Context, logger *zap.Logger, opt Options, key []byte) error {
		stores := opt.getRegistryStores(logger)
		for _, store := range stores {
			err := store.CleanRegistryData()
			if err != nil {
				return err
			}
		}
		return opt.Db.Set(migrationsPrefix, key, migrationCompleted)
	},
}
