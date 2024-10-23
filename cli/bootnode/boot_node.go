package bootnode

import (
	"fmt"
	"log"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/utils/commons"

	"github.com/ssvlabs/ssv/logging"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	globalcfg "github.com/ssvlabs/ssv/cli/config"
	bootnode "github.com/ssvlabs/ssv/utils/boot_node"
)

type config struct {
	globalcfg.Global `yaml:"global"`
	Options          bootnode.Options `yaml:"bootnode"`
}

var cfg config

var globalArgs globalcfg.Args

// StartBootNodeCmd is the command to start SSV boot node
var StartBootNodeCmd = &cobra.Command{
	Use:   "start-boot-node",
	Short: "Starts boot node for discovery based ENR",
	Run: func(cmd *cobra.Command, args []string) {
		commons.SetBuildData(cmd.Parent().Short, cmd.Parent().Version)

		err := globalcfg.Prepare(&cfg, &globalArgs)
		if err != nil {
			log.Fatalf("could not prepare config: %v", err)
		}

		err = logging.SetGlobalLogger(
			cfg.LogLevel,
			cfg.LogLevelFormat,
			cfg.LogFormat,
			&logging.LogFileOptions{
				FileName:   cfg.LogFilePath,
				MaxSize:    cfg.LogFileSize,
				MaxBackups: cfg.LogFileBackups,
			},
		)
		if err != nil {
			log.Fatalf("could not create logger: %v", err)
		}
		logger := zap.L()
		defer logging.CapturePanic(logger)

		logger.Info(fmt.Sprintf("starting %v", commons.GetBuildData()))

		networkConfig, err := networkconfig.GetNetworkConfigByName(cfg.Options.Network)
		if err != nil {
			logger.Fatal("failed to get network config", zap.Error(err))
		}
		bootNode, err := bootnode.New(networkConfig, cfg.Options)
		if err != nil {
			logger.Fatal("failed to set up boot node", zap.Error(err))
		}
		if err := bootNode.Start(cmd.Context(), logger); err != nil {
			logger.Fatal("failed to start boot node", zap.Error(err))
		}
	},
}

func init() {
	globalcfg.ProcessArgs(&cfg, &globalArgs, StartBootNodeCmd)
}
