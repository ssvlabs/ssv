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
		go handleShutdownSignals(sigC, logger, cancel, func(sig os.Signal) {
			logger.Fatal("received second shutdown signal, exiting immediately", zap.String("signal", sig.String()))
		})

		// Build the node, then supervise it: supervise blocks until the first terminal event (a
		// service/bring-up failure, or the signal canceling ctx) brings the node down.
		n, err := buildNode(ctx, &cfg, logger)
		if err == nil {
			err = supervise(ctx, logger, shutdownGraceTimeout, n.start, n.close)
		}
		if err != nil {
			// buildNode/supervise surface the terminal cause — for a deliberate stop that's
			// context.Canceled, or whatever was in flight when the signal landed. Key on whether a
			// signal arrived (ctx), not the error's nature: a deliberate stop exits 0 (running the
			// deferred observability shutdown) even if a genuine error coincided — whoever sent the
			// signal isn't restarting on exit code anyway.
			if ctx.Err() != nil {
				logger.Info("node stopped on signal", startupErrorLogFields(err)...)
				return
			}
			logger.Fatal("could not start node", startupErrorLogFields(err)...)
		}
	},
}

// handleShutdownSignals implements the two-stage stop scoped to start-node: the first signal calls
// gracefulStop (cancel the node ctx so shutdown unwinds cleanly), and a second one calls forceStop —
// the escape hatch for a graceful teardown that wedged. forceStop is injected (it's logger.Fatal in
// production, which os.Exits) so the two-stage behavior is testable.
func handleShutdownSignals(sigC <-chan os.Signal, logger *zap.Logger, gracefulStop func(), forceStop func(os.Signal)) {
	sig := <-sigC
	logger.Info("received shutdown signal, shutting down gracefully (repeat to force-exit)", zap.String("signal", sig.String()))
	gracefulStop()

	sig = <-sigC
	forceStop(sig)
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
