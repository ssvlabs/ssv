package tests

import (
	"log"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// RootCmd represents the root command of SSV CLI
var RootCmd = &cobra.Command{
	Use:   "ssvnodetests",
	Short: "ssv-node-test",
	Long:  `SSV node test is a CLI for running SSV-related operations of testing.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
	},
}

// Execute executes the root command
func Execute(appName, version string) {
	RootCmd.Short = appName
	RootCmd.Version = version

	if err := RootCmd.Execute(); err != nil {
		log.Fatal("failed to execute root command", zap.Error(err))
	}
}

func init() {
	RootCmd.AddCommand(RsaCmd)
}
