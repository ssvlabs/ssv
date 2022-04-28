package scenarios

import (
	"context"
	"fmt"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// Run runs a scenario with the given bootstrapper
func Run(pctx context.Context, bootstrapper Bootstrapper, scenario Scenario) error {
	ctx, cancel := context.WithCancel(pctx)
	defer cancel()
	loggerFactory := func(s string) *zap.Logger {
		return logex.GetLogger(zap.String("who", s))
	}
	logger := loggerFactory(fmt.Sprintf("RUNNER/%s", scenario.Name()))
	logger.Info("bootstrapping")
	sctx, err := bootstrapper(ctx)
	if err != nil {
		return errors.Wrap(err, "could not bootstrap")
	}

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
