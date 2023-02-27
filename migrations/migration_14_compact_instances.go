package migrations

import (
	"context"
)

var migrationCompactInstances = Migration{
	Name: "migration_14_compact_instances",
	Run: func(ctx context.Context, opt Options, key []byte) error {
		return nil
	},
}
