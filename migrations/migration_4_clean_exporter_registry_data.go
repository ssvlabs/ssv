package migrations

import (
	"context"
	"github.com/pkg/errors"
)

var migrationCleanExporterRegistryData = Migration{
	Name: "migration_4_clean_exporter_registry_data",
	Run: func(ctx context.Context, opt Options, key []byte) error {
		storage := opt.exporterStorage()
		err := storage.CleanRegistryData()
		if err != nil {
			return errors.Wrap(err, "could not clean registry data")
		}
		return opt.Db.Set(migrationsPrefix, key, migrationCompleted)
	},
}
