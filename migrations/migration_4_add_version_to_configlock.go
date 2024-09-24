package migrations

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/storage/basedb"
)

// This migration adds a version field to the ConfigLock structure
var migration_4_add_version_to_configlock = Migration{
	Name: "migration_4_add_version_to_configlock",
	Run: func(ctx context.Context, logger *zap.Logger, opt Options, key []byte, completed CompletedFunc) error {
		return opt.Db.Update(func(txn basedb.Txn) error {
			nodeStorage, err := opt.nodeStorage(logger)
			if err != nil {
				return fmt.Errorf("failed to get node storage: %w", err)
			}

			config, found, err := nodeStorage.GetConfig(txn)
			if err != nil {
				return fmt.Errorf("failed to get config: %w", err)
			}

			// If config is not found, skip the migration
			if !found {
				return nil
			}

			// If config is found, check if version is missing and update it
			if config.Version == "" {
				config.Version = opt.Version

				// Save the updated config back atomically
				if err := nodeStorage.SaveConfig(txn, config); err != nil {
					return fmt.Errorf("failed to save config: %w", err)
				}

				logger.Debug("ConfigLock updated with version", zap.String("version", config.Version))
			} else if found {
				logger.Debug("ConfigLock already has version", zap.String("version", config.Version))
			}

			return completed(txn)
		})
	},
}
