package cli

import (
	"github.com/bloxapp/ssv/cli/bootnode"
	"github.com/bloxapp/ssv/cli/exporter"
	"github.com/bloxapp/ssv/cli/ssvnode"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"log"
)

// Logger is the default logger
var Logger *zap.Logger

// RootCmd represents the root command of SSV CLI
var RootCmd = &cobra.Command{
	Use:  "ssvnode",
	Short:  "ssv-node",
	Long: `SSV node is a CLI for running SSV-related operations.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string){
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

func init()  {
	RootCmd.AddCommand(bootnode.StartBootNodeCmd)
	RootCmd.AddCommand(exporter.StartExporterNodeCmd)
	RootCmd.AddCommand(ssvnode.StartNodeCmd)
}