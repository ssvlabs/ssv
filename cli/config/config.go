package config

import (
	"fmt"

	"github.com/ilyakaznacheev/cleanenv"
	"github.com/spf13/cobra"
)

// Args expose available global args for cli command
type Args struct {
	// ConfigPath is a path to the main configuration file.
	ConfigPath string
	// ShareConfigPath is an additional config file (path) that (if present) will overwrite
	// configuration supplied by the config file at ConfigPath.
	ShareConfigPath string
}

// Global expose available global config for cli command
type Global struct {
	LogLevel       string `yaml:"LogLevel" env:"LOG_LEVEL" env-description:"Defines logger's log level"`
	LogFormat      string `yaml:"LogFormat" env:"LOG_FORMAT" env-description:"Defines logger's encoding, valid values are 'json' and 'console' (default)"`
	LogLevelFormat string `yaml:"LogLevelFormat" env:"LOG_LEVEL_FORMAT" env-description:"Defines logger's level format, valid values are 'capitalColor' (default), 'capital' or 'lowercase'"`
	LogFilePath    string `yaml:"LogFilePath" env:"LOG_FILE_PATH" env-description:"File path to write logs to"`
	LogFileSize    int    `yaml:"LogFileSize" env:"LOG_FILE_SIZE" env-description:"Maximum log file size in megabytes before rotation"`
	LogFileBackups int    `yaml:"LogFileBackups" env:"LOG_FILE_BACKUPS" env-description:"Number of rotated log files to keep"`
}

// Defaulter is implemented by config types that seed their defaults in code (via ApplyDefaults)
// instead of cleanenv `env-default` struct tags. Callers must apply it before reading config so an
// explicit YAML/env value wins over the default — including a zero value like false or 0, which
// env-default can't preserve (a bool's zero value is false, so env-default:"true" is
// indistinguishable from an explicit false; #2868).
type Defaulter interface {
	ApplyDefaults()
}

func (g *Global) ApplyDefaults() {
	g.LogLevel = "info"
	g.LogFormat = "console"
	g.LogLevelFormat = "capitalColor"
	g.LogFilePath = "./data/debug.log"
	g.LogFileSize = 500
	g.LogFileBackups = 3
}

// ProcessArgs processes and handles CLI arguments
func ProcessArgs(cfg any, a *Args, cmd *cobra.Command) {
	configFlag := "config"
	cmd.PersistentFlags().StringVarP(&a.ConfigPath, configFlag, "c", "./config/config.yaml", "Path to configuration file")
	_ = cmd.MarkFlagRequired(configFlag)

	shareConfigFlag := "share-config"
	cmd.PersistentFlags().StringVarP(&a.ShareConfigPath, shareConfigFlag, "s", "", "Path to local share configuration file")
	_ = cmd.MarkFlagRequired(shareConfigFlag)

	cmd.SetUsageTemplate(describeHelp(cfg) + "\n" + cmd.UsageTemplate())
}

func Prepare(cfg any, a *Args) error {
	// Seed defaults before reading, so an explicit YAML/env value wins over the default (see Defaulter).
	if d, ok := cfg.(Defaulter); ok {
		d.ApplyDefaults()
	}

	if a.ConfigPath != "" {
		err := cleanenv.ReadConfig(a.ConfigPath, cfg)
		if err != nil {
			return fmt.Errorf("could not read config: %w", err)
		}
	}
	if a.ShareConfigPath != "" {
		err := cleanenv.ReadConfig(a.ShareConfigPath, cfg)
		if err != nil {
			return fmt.Errorf("could not read share config: %w", err)
		}
	}

	if a.ConfigPath == "" && a.ShareConfigPath == "" {
		// No config file: read from env vars only. Defaults were already seeded above via ApplyDefaults.
		err := cleanenv.ReadEnv(cfg)
		if err != nil {
			return fmt.Errorf("could not set up config based on environment variables and struct defaults: %w", err)
		}
	}

	return nil
}
