package runner

import (
	"context"
	"go.uber.org/zap"
)

func Start(logger *zap.Logger, scenario Scenario, bootstrapper Bootstrapper) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sctx, err := bootstrapper(ctx, logger, scenario)
	if err != nil {
		logger.Panic("could not bootstrap scenario", zap.Error(err))
	}
	if err := run(logger, scenario, sctx); err != nil {
		logger.Panic("could not run scenario", zap.Error(err))
	}
}

func run(logger *zap.Logger, scenario Scenario, sctx *ScenarioContext) error {
	logger.Info("all resources were created, starting pre-execution of the scenario")
	if err := scenario.PreExecution(sctx); err != nil {
		return err
	}
	logger.Info("executing scenario")
	if err := scenario.Execute(sctx); err != nil {
		return err
	}
	logger.Info("running post-execution of the scenario")
	if err := scenario.PostExecution(sctx); err != nil {
		return err
	}
	logger.Info("done")

	return nil
}
