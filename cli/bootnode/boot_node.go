package bootnode

import (
	"fmt"
	"github.com/bloxapp/ssv/utils/commons"
	"log"

	"github.com/bloxapp/ssv/logging"

	"github.com/ilyakaznacheev/cleanenv"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	global_config "github.com/bloxapp/ssv/cli/config"
	bootnode "github.com/bloxapp/ssv/utils/boot_node"
)

type config struct {
	global_config.GlobalConfig `yaml:"global"`
	Options                    bootnode.Options `yaml:"bootnode"`
}

var cfg config

var globalArgs global_config.Args

// StartBootNodeCmd is the command to start SSV boot node
var StartBootNodeCmd = &cobra.Command{
	Use:   "start-boot-node",
	Short: "Starts boot node for discovery based ENR",
	Run: func(cmd *cobra.Command, args []string) {
		commons.SetBuildData(cmd.Parent().Short, cmd.Parent().Version)

		if err := cleanenv.ReadConfig(globalArgs.ConfigPath, &cfg); err != nil {
			log.Fatal(err)
		}

		err := logging.SetGlobalLogger(
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
			log.Fatal(err)
		}

		logger := zap.L()

		logger.Info(fmt.Sprintf("starting %v", commons.GetBuildData()))

		bootNode, err := bootnode.New(cfg.Options)
		if err != nil {
			logger.Fatal("failed to set up boot node", zap.Error(err))
		}
		if err := bootNode.Start(cmd.Context(), logger); err != nil {
			logger.Fatal("failed to start boot node", zap.Error(err))
		}
	},
}

func init() {
	global_config.ProcessArgs(&cfg, &globalArgs, StartBootNodeCmd)
}
