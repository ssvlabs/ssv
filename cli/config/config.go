package config

import (
	"github.com/ilyakaznacheev/cleanenv"
	"github.com/spf13/cobra"
)

type Args struct {
	ConfigPath string
	ShareConfigPath string
}

type GlobalConfig struct {
	LogLevel string `yaml:"LogLevel" env:"LOG_LEVEL" env-default:"info" env-description:"Defines logger's log level'"`
}

// ProcessArgs processes and handles CLI arguments
func ProcessArgs(cfg interface{}, a *Args, cmd *cobra.Command) {
	configFlag := "config"
	cmd.PersistentFlags().StringVarP(&a.ConfigPath, configFlag, "c", "./config/config.yaml", "Path to configuration file")
	cmd.MarkFlagRequired(configFlag)

	shareConfigFlag := "share-config"
	cmd.PersistentFlags().StringVarP(&a.ShareConfigPath, shareConfigFlag, "s", "", "Path to local share configuration file")
	cmd.MarkFlagRequired(shareConfigFlag)

	envHelp, _ := cleanenv.GetDescription(cfg, nil)
	cmd.SetUsageTemplate(envHelp + "\n" + cmd.UsageTemplate())

}