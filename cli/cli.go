package cli

import (
	"github.com/bloxapp/ssv/cli/flags"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/ilyakaznacheev/cleanenv"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"log"
)

// Logger is the default logger
var Logger *zap.Logger

type Args struct {
	ConfigPath string
}

// ProcessArgs processes and handles CLI arguments
func ProcessArgs(cfg interface{}, a *Args, cmd *cobra.Command) {
	configFlag := "config"
	cmd.PersistentFlags().StringVarP(&a.ConfigPath, configFlag, "c", "./config/config.yaml", "Path to configuration file")
	cmd.MarkFlagRequired(configFlag)

	envHelp, _ := cleanenv.GetDescription(cfg, nil)
	cmd.SetUsageTemplate(envHelp + "\n" + cmd.UsageTemplate())

}

// RootCmd represents the root command of SSV CLI
var RootCmd = &cobra.Command{
	Use:  "ssvnode",
	Short:  "ssv-node",
	Long: `SSV node is a CLI for running SSV-related operations.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string){
		if err := cleanenv.ReadConfig(globalArgs.ConfigPath, &cfg); err != nil {
			log.Fatal(err)
		}
		loggerLevel, err := flags.GetLoggerLevelValue(cmd)
		if err != nil {
			log.Fatal("failed to get logger level flag value", zap.Error(err))
		}
		Logger = logex.Build(cmd.Parent().Short, loggerLevel)
	},
}

// Execute executes the root command
func Execute(appName, version string) {
	RootCmd.Short = appName
	RootCmd.Version = version
	flags.AddLoggerLevelFlag(RootCmd)
	if err := RootCmd.Execute(); err != nil {
		log.Fatal("failed to execute root command", zap.Error(err))
	}
}