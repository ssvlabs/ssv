package migrations

import (
	"context"

	"go.uber.org/zap"
)

var migrationCleanRegistryDataIncludingSignerStorage = Migration{
	Name: "migration_12_clean_registry_data_including_signer_storage",
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
