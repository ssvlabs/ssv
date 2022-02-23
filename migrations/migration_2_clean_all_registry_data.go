package migrations

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// This migration is responsible to delete all (exporter, operator) registry data
var migrationCleanAllRegistryData = Migration{
	Name: "migration_2_clean_all_registry_data",
	Run: func(ctx context.Context, opt Options, key []byte) error {
		// check if deprecated migration (ownerAddrAndOperatorsPKsMigration) was applied
		fullPath := filepath.Clean(fmt.Sprintf("%s/oa_pks/migration.txt", opt.DbPath))
		if _, err := os.Stat(fullPath); errors.Is(err, os.ErrNotExist) {
			stores := opt.getRegistryStores()
			for _, store := range stores {
				err := store.CleanRegistryData()
				if err != nil {
					return err
				}
			}
		} else {
			opt.Logger.Debug("migration should be fake applied due to completed oa_pks deprecated migration, setting as completed")
		}
		return opt.Db.Set(migrationsPrefix, key, migrationCompleted)
	},
}
