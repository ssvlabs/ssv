package migrations

import (
	"context"
	"go.uber.org/zap"
)

var migrationCleanValidatorRegistryData = Migration{
	Name: "migration_5_clean_validator_registry_data",
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
