package tests

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"

	"github.com/bloxapp/ssv/automation/qbft/scenarios"
	"github.com/bloxapp/ssv/utils/logex"
)

func Test_Automation_QBFTScenarios(t *testing.T) {
	logger := logex.Build("simulation", zapcore.DebugLevel, nil)

	scenariosToRun := []*scenarios.IntegrationTest{
		scenarios.Regular(logger),
	}

	for _, scenario := range scenariosToRun {
		require.NoError(t, scenario.Run())
	}
}
