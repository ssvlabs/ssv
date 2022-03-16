package migrations

import (
	"context"
	"github.com/pkg/errors"
)

// This migration is responsible to delete all (exporter, operator) registry data
var migrationCleanOperatorNodeRegistryData = Migration{
	Name: "migration_3_clean_operator_node_registry_data",
	Run: func(ctx context.Context, opt Options, key []byte) error {
		storage := opt.nodeStorage()
		err := storage.CleanRegistryData()
		if err != nil {
			return errors.Wrap(err, "could not clean registry data")
		}
		return opt.Db.Set(migrationsPrefix, key, migrationCompleted)
	},
}
