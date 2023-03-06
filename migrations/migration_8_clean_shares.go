package migrations

import (
	"context"
	"go.uber.org/zap"
)

var migrationCleanShares = Migration{
	Name: "migration_8_clean_shares",
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
