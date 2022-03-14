package migrations

import (
	"context"
)

// This migration is responsible to delete all (exporter, operator) registry data
var migrationTemp = Migration{
	Name: "migration_4_temp",
	Run: func(ctx context.Context, opt Options, key []byte) error {
		return opt.Db.Delete(migrationsPrefix, []byte("migration_3_clean_operator_node_registry_data"))
	},
}
