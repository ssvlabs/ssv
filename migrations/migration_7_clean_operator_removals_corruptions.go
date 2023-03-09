package migrations

import (
	"context"

	"go.uber.org/zap"
)

var migrationCleanOperatorRemovalCorruptions = Migration{
	Name: "migration_7_clean_operator_removal_corruptions",
	Run: func(ctx context.Context, logger *zap.Logger, opt Options, key []byte) error {
		nodeStorage := opt.nodeStorage(logger)
		if err := nodeStorage.CleanRegistryData(); err != nil {
			return err
		}
		return opt.Db.Set(migrationsPrefix, key, migrationCompleted)
	},
}
