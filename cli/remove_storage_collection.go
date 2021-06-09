package cli

import (
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/cli/flags"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// removeStorageCollectionCmd is the command to remove storage by prefix (collection)
var removeStorageCollectionCmd = &cobra.Command{
	Use:   "remove-storage-collection",
	Short: "removing storage collection",
	Run: func(cmd *cobra.Command, args []string) {
		logger := logex.Build(RootCmd.Short, zapcore.DebugLevel)

		role, err := flags.GetRoleFlagValue(cmd)
		if role == 100{
			logger.Error("role not exist", zap.Int("role", role))
			return
		}

		opt := basedb.Options{
			Type:   "badger-db",
			Path:   "./data/db",
			Logger: logger,
		}
		db, err := kv.New(opt)
		if err != nil {
			logger.Error("failed to create db", zap.Error(err))
			return
		}
		ibftStorage := collections.NewIbft(db, logger, beacon.Role(role).String())
		err = ibftStorage.RemoveAll()
		if err != nil {
			logger.Error("failed to remove all", zap.Error(err))
			return
		}
	},
}

func init() {
	RootCmd.AddCommand(removeStorageCollectionCmd)
}
