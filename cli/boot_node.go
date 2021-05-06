package cli

import (
	bootnode "github.com/bloxapp/ssv/utils/boot_node"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

type Config struct {
	Options bootnode.Options `yaml:"bootnode"`
}

var cfg Config

var globalArgs Args

// startBootNodeCmd is the command to start SSV boot node
var startBootNodeCmd = &cobra.Command{
	Use:   "start-boot-node",
	Short: "Starts boot node for discovery based ENR",
	Run: func(cmd *cobra.Command, args []string) {
		Logger.Info(globalArgs.ConfigPath)
		cfg.Options.Logger = Logger

		bootNode := bootnode.New(cfg.Options)
		if err := bootNode.Start(cmd.Context()); err != nil {
			Logger.Fatal("failed to start boot node", zap.Error(err))
		}
	},
}

func init() {
	ProcessArgs(&cfg, &globalArgs, startBootNodeCmd)
	RootCmd.AddCommand(startBootNodeCmd)
}
