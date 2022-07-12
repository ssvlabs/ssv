package main

import (
	"github.com/bloxapp/ssv/automation/qbft/runner"
	"github.com/bloxapp/ssv/automation/qbft/scenarios"
	"github.com/bloxapp/ssv/utils/logex"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"time"
)

func main() {
	logger := logex.Build("simulation", zapcore.DebugLevel, nil)
	var scenariosToRun []string

	// TODO: add flag to read in parallel on multiple scenarios

	for _, s := range scenariosToRun {
		scenario := scenarios.NewScenario(s, logger)
		runner.Start(logger, scenario, scenarios.QBFTScenarioBootstrapper())
		logger.Info("finished scenario", zap.String("name", scenario.Name()))
		<-time.After(3 * time.Second)
	}
}
