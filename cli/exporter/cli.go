package exporter

import (
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"log"
)

// Logger is the default logger
var Logger *zap.Logger

// Cmd represents the root command of Exporter Node CLI
var Cmd = &cobra.Command{
	Use:  "exporter",
	Short:  "exporter-node",
	Long: `Exporter node is a CLI for running SSV exporter.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string){
	},
}

// Execute executes the root command
func Execute(appName, version string) {
	Cmd.Short = appName
	Cmd.Version = version

	if err := Cmd.Execute(); err != nil {
		log.Fatal("failed to execute root command", zap.Error(err))
	}
}

func init()  {
	Cmd.AddCommand(StartExporterNodeCmd)
}