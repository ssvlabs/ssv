package migrations

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

var migration_3_drop_registry_data = Migration{
	Name: "migration_3_drop_registry_data",
	Run: func(ctx context.Context, logger *zap.Logger, opt Options, key []byte, completed CompletedFunc) error {
		storage, err := opt.nodeStorage(logger)
		if err != nil {
			return fmt.Errorf("failed to get node storage: %w", err)
		}
		err = storage.DropRegistryData()
		if err != nil {
			return fmt.Errorf("failed to drop registry data: %w", err)
		}
		return completed(opt.Db)
	},
}
