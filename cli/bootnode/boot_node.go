package bootnode

import (
	"fmt"
	"github.com/bloxapp/ssv/cli/config"
	bootnode "github.com/bloxapp/ssv/utils/boot_node"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/ilyakaznacheev/cleanenv"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"log"
)

type bootNodeConfig struct {
	config.GlobalConfig `yaml:"global"`
	Options bootnode.Options `yaml:"bootnode"`
}

var cfg bootNodeConfig

var globalArgs config.Args

// startBootNodeCmd is the command to start SSV boot node
var StartBootNodeCmd = &cobra.Command{
	Use:   "start-boot-node",
	Short: "Starts boot node for discovery based ENR",
	Run: func(cmd *cobra.Command, args []string) {
		if err := cleanenv.ReadConfig(globalArgs.ConfigPath, &cfg); err != nil {
			log.Fatal(err)
		}

		loggerLevel, err := logex.GetLoggerLevelValue(cfg.LogLevel)
		Logger := logex.Build(cmd.Parent().Short, loggerLevel)

		if err != nil {
			Logger.Warn(fmt.Sprintf("Default log level set to %s", loggerLevel),zap.Error(err))
		}

		cfg.Options.Logger = Logger
		bootNode := bootnode.New(cfg.Options)
		if err := bootNode.Start(cmd.Context()); err != nil {
			Logger.Fatal("failed to start boot node", zap.Error(err))
		}
	},
}

func init() {
	config.ProcessArgs(&cfg, &globalArgs, StartBootNodeCmd)
}
