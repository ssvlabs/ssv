package migrations

import (
	"context"
)

// This migration is responsible to delete all (exporter, operator) registry data
var migrationCleanAllRegistryData = Migration{
	Name: "migration_2_clean_all_registry_data",
	Run: func(ctx context.Context, opt Options, key []byte) error {
		stores := opt.getRegistryStores()
		for _, store := range stores {
			err := store.CleanRegistryData()
			if err != nil {
				return err
			}
		}
		return opt.Db.Set(migrationsPrefix, key, migrationCompleted)
	},
}
