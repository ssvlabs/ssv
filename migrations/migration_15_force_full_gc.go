package migrations

import (
	"context"

	"go.uber.org/zap"
)

var migrationForceFullGC = Migration{
	Name: "migration_15_force_full_gc",
	Run: func(ctx context.Context, logger *zap.Logger, opt Options, key []byte) error {
		return nil
	},
}
