package config

import (
	"github.com/ilyakaznacheev/cleanenv"
	"github.com/spf13/cobra"
)

// Args expose available global args for cli command
type Args struct {
	ConfigPath      string
	ShareConfigPath string
}

// GlobalConfig expose available global config for cli command
type GlobalConfig struct {
	LogLevel       string `yaml:"LogLevel" env:"LOG_LEVEL" env-default:"info" env-description:"Log level (debug, info, warn, error)"`
	LogFormat      string `yaml:"LogFormat" env:"LOG_FORMAT" env-default:"console" env-description:"Log encoding format (json, console)"`
	LogLevelFormat string `yaml:"LogLevelFormat" env:"LOG_LEVEL_FORMAT" env-default:"capitalColor" env-description:"Log level format (capitalColor, capital, lowercase)"`
	LogFilePath    string `yaml:"LogFilePath" env:"LOG_FILE_PATH" env-default:"./data/debug.log" env-description:"File path to write logs to"`
	LogFileSize    int    `yaml:"LogFileSize" env:"LOG_FILE_SIZE" env-default:"500" env-description:"Maximum log file size in megabytes before rotation"`
	LogFileBackups int    `yaml:"LogFileBackups" env:"LOG_FILE_BACKUPS" env-default:"3" env-description:"Number of rotated log files to keep"`
}

// ProcessArgs processes and handles CLI arguments
func ProcessArgs(cfg interface{}, a *Args, cmd *cobra.Command) {
	configFlag := "config"
	cmd.PersistentFlags().StringVarP(&a.ConfigPath, configFlag, "c", "./config/config.yaml", "Path to configuration file")
	_ = cmd.MarkFlagRequired(configFlag)

	shareConfigFlag := "share-config"
	cmd.PersistentFlags().StringVarP(&a.ShareConfigPath, shareConfigFlag, "s", "", "Path to local share configuration file")
	_ = cmd.MarkFlagRequired(shareConfigFlag)

	envHelp, _ := cleanenv.GetDescription(cfg, nil)
	cmd.SetUsageTemplate(envHelp + "\n" + cmd.UsageTemplate())

}
