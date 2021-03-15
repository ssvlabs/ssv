package cli

import (
	bootnode "github.com/bloxapp/ssv/utils/boot_node"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// startNodeCmd is the command to start SSV node
var startBootNodeCmd = &cobra.Command{
	Use:   "start-boot-node",
	Short: "Starts boot node for discovery based ENR",
	Run: func(cmd *cobra.Command, args []string) {
		logger := Logger.With(zap.Uint64("boot_node_id", 5))
		logger.Info("Hello")

		bootNode := bootnode.New(bootnode.Options{
			NodeID: 5,
			Logger: logger,
		})

		if err := bootNode.Start(cmd.Context()); err != nil {
			logger.Fatal("failed to start boot node", zap.Error(err))
		}
	},
}

func init() {
	RootCmd.AddCommand(startBootNodeCmd)
}
