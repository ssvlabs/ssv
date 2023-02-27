package migrations

import (
	"context"
)

var migrationForceFullGC = Migration{
	Name: "migration_15_force_full_gc",
	Run: func(ctx context.Context, opt Options, key []byte) error {
		return nil
	},
}
