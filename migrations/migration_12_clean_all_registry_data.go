package migrations

import (
	"context"
)

var migrationCleanRegistryDataIncludingSignerStorage = Migration{
	Name: "migration_12_clean_registry_data_including_signer_storage",
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
