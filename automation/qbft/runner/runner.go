package runner

import (
	"context"
	"testing"

	"go.uber.org/zap"
)

// Start starts the runner.
func Start(t *testing.T, logger *zap.Logger, scenario Scenario, bootstrapper Bootstrapper) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sctx, err := bootstrapper(ctx, logger, scenario)
	if err != nil {
		logger.Panic("could not bootstrap scenario", zap.Error(err))
	}

	scenario.ApplyCtx(sctx)

	logger.Info("all resources were created, starting execution of the scenario")

	if err := scenario.Run(t); err != nil {
		logger.Panic("could not run scenario", zap.Error(err))
	}

	logger.Info("done")
}
