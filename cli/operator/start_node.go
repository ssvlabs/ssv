package operator

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	globalcfg "github.com/ssvlabs/ssv/cli/config"
	"github.com/ssvlabs/ssv/observability"
	ssvlog "github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/utils/commons"
)

var cfg config

var globalArgs globalcfg.Args

func init() {
	globalcfg.ProcessArgs(&cfg, &globalArgs, StartNodeCmd)
}

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
			log.Fatalf("could not initialize observability configuration: %v", err)
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

		// Cancel the node ctx on SIGINT/SIGTERM so shutdown unwinds gracefully through the errgroup
		// (long-lived services return nil, node.Close() runs) instead of the process being terminated
		// abruptly. Scoped to start-node so other subcommands keep cobra's default signal behavior.
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		if err := runNode(ctx, &cfg, logger); err != nil {
			// A signal during the synchronous startup phase surfaces as an error rather than the
			// errgroup's clean-cancel (nil) path; treat it as a deliberate stop, not a startup failure
			// — exit 0 (letting the deferred observability shutdown run) instead of Fatal.
			if ctx.Err() != nil {
				logger.Info("node shut down before startup completed", zap.Error(err))
				return
			}
			logger.Fatal("could not start node", startupErrorLogFields(err)...)
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
