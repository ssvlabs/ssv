package migrations

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/storage/basedb"
)

// This migration adds the Alan fork name to the network name
var migration_4_configlock_add_alan_fork_to_network_name = Migration{
	Name: "migration_4_configlock_add_alan_fork_to_network_name",
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

			// If config is not found, it means the node is not initialized yet
			if found {
				if err := nodeStorage.SaveConfig(txn, config); err != nil {
					return fmt.Errorf("failed to save config: %w", err)
				}
			}

			return completed(txn)
		})
	},
}
