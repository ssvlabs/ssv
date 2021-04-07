package cli

import (
	"github.com/bloxapp/ssv/cli/flags"
	bootnode "github.com/bloxapp/ssv/utils/boot_node"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// startNodeCmd is the command to start SSV node
var startBootNodeCmd = &cobra.Command{
	Use:   "start-boot-node",
	Short: "Starts boot node for discovery based ENR",
	Run: func(cmd *cobra.Command, args []string) {
		logger := Logger.Named("boot-node")

		privateKey, err := flags.GetBootNodePrivateKeyFlagValue(cmd)
		if err != nil {
			logger.Fatal("failed to get private key flag value", zap.Error(err))
		}

		externalIP, err := flags.GetExternalIPFlagValue(cmd)
		if err != nil {
			logger.Fatal("failed to get external ip flag value", zap.Error(err))
		}

		bootNode := bootnode.New(bootnode.Options{
			Logger:     logger,
			PrivateKey: privateKey,
			ExternalIP: externalIP,
		})

		if err := bootNode.Start(cmd.Context()); err != nil {
			logger.Fatal("failed to start boot node", zap.Error(err))
		}
	},
}

func init() {
	flags.AddBootNodePrivateKeyFlag(startBootNodeCmd)
	flags.AddExternalIPFlag(startBootNodeCmd)
	RootCmd.AddCommand(startBootNodeCmd)
}
