package cli

import (
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger is the default logger
var Logger *zap.Logger

// RootCmd represents the root command of SSV CLI
var RootCmd = &cobra.Command{
	Use:  "ssv-cli",
	Long: `ssv-cli is a CLI for running SSV-related operations.`,
}

// Execute executes the root command
func Execute(appName, version string) {
	Logger = logex.Build(appName, zapcore.DebugLevel)
	RootCmd.Short = appName
	RootCmd.Version = version

	if err := RootCmd.Execute(); err != nil {
		Logger.Fatal("failed to execute root command", zap.Error(err))
	}
}
