package main

import (
	"github.com/bloxapp/ssv/automation/qbft/runner"
	"github.com/bloxapp/ssv/automation/qbft/scenarios"
	"github.com/bloxapp/ssv/utils/logex"
	"go.uber.org/zap/zapcore"
)

func main() {
	logger := logex.Build("simulation", zapcore.DebugLevel, nil)
	scenariosToRun := []string{
		scenarios.OnForkV1Scenario,
		// TODO: read from ENV
	}

	// TODO: add flag to read in parallel on multiple scenarios

	for _, s := range scenariosToRun {
		scenario := scenarios.NewScenario(s, logger)
		runner.Start(logger, scenario)
	}
}
