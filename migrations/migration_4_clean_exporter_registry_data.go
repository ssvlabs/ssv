package migrations

import (
	"context"

	"go.uber.org/zap"

	"github.com/pkg/errors"
)

var migrationCleanExporterRegistryData = Migration{
	Name: "migration_4_clean_exporter_registry_data",
	Run: func(ctx context.Context, logger *zap.Logger, opt Options, key []byte) error {
		storage := opt.nodeStorage(logger)
		err := storage.CleanRegistryData()
		if err != nil {
			return errors.Wrap(err, "could not clean registry data")
		}
		return opt.Db.Set(migrationsPrefix, key, migrationCompleted)
	},
}
