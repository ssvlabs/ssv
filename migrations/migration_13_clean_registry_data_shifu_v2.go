package migrations

import (
	"context"
)

var migrationCleanRegistryDataShifuV2 = Migration{
	Name: "migration_13_clean_registry_data_shifu_v2",
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
