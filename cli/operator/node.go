package operator

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability"
	ssvlog "github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/utils/commons"
)

// StartNodeCmd is the command to start SSV node
var StartNodeCmd = &cobra.Command{
	Use:   "start-node",
	Short: "Starts an instance of SSV node",
	Run: func(cmd *cobra.Command, args []string) {
		commons.SetBuildData(cmd.Parent().Short, cmd.Parent().Version)

		if err := cfg.load(globalArgs.ConfigPath, globalArgs.ShareConfigPath); err != nil {
			log.Fatal(err)
		}

		observabilityShutdown, err := observability.Initialize(
			cmd.Context(),
			cmd.Parent().Short,
			cmd.Parent().Version,
			buildObservabilityOptions(&cfg)...)
		if err != nil {
			log.Fatal("could not initialize observability configuration", zap.Error(err))
		}

		logger := zap.L()
		defer ssvlog.CapturePanic(logger)

		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err = observabilityShutdown(shutdownCtx); err != nil {
				logger.Error("could not shutdown observability stack", zap.Error(err))
			}
		}()

		logger.Info(fmt.Sprintf("starting %v", commons.GetBuildData()))

		if err := run(cmd.Context(), &cfg, logger); err != nil {
			logger.Fatal("failed to run node", zap.Error(err))
		}
	},
}

// buildObservabilityOptions assembles the observability stack options (logger plus
// optional metrics and traces) from config, keeping the StartNodeCmd bootstrap thin.
func buildObservabilityOptions(cfg *config) []observability.Option {
	opts := []observability.Option{
		observability.WithLogger(
			cfg.LogLevel,
			cfg.LogLevelFormat,
			cfg.LogFormat,
			cfg.LogFilePath,
			cfg.LogFileSize,
			cfg.LogFileBackups,
		),
	}
	if cfg.MetricsAPIPort > 0 {
		opts = append(opts, observability.WithMetrics())
	}
	if cfg.EnableTraces {
		opts = append(opts, observability.WithTraces())
	}
	return opts
}
