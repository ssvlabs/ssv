package tests

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/automation/qbft/scenarios"
)

func Test_Automation_QBFTScenarios(t *testing.T) {
	scenariosToRun := []*scenarios.IntegrationTest{
		scenarios.Regular(),
	}

	for _, scenario := range scenariosToRun {
		require.NoError(t, scenario.Run())
	}
}
