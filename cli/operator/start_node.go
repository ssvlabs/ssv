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

		// First SIGINT/SIGTERM cancels the node ctx so shutdown unwinds gracefully (services stop,
		// node Close runs); a second one force-exits via Fatal — the escape hatch for a graceful
		// teardown that wedged. Scoped to start-node so other subcommands keep cobra's default
		// signal behavior.
		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()
		sigC := make(chan os.Signal, 2)
		signal.Notify(sigC, os.Interrupt, syscall.SIGTERM)
		go func() {
			sig := <-sigC
			logger.Info("received shutdown signal, shutting down gracefully (repeat to force-exit)", zap.String("signal", sig.String()))
			cancel()
			sig = <-sigC
			logger.Fatal("received second shutdown signal, exiting immediately", zap.String("signal", sig.String()))
		}()

		if err := runNode(ctx, &cfg, logger); err != nil {
			// runNode surfaces the terminal cause — for a deliberate stop that's context.Canceled,
			// or whatever was in flight when the signal landed. Key on whether a signal arrived, not
			// the error's nature: a deliberate stop exits 0 (running the deferred observability
			// shutdown) even if a genuine error coincided — whoever sent the signal isn't restarting
			// on exit code anyway.
			if ctx.Err() != nil {
				logger.Info("node stopped on signal", startupErrorLogFields(err)...)
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
