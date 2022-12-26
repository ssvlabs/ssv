package tests

import (
	"testing"

	"github.com/bloxapp/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/automation/qbft/scenarios"
)

func Test_Automation_QBFTScenarios(t *testing.T) {
	// _ = logging.SetLogLevelRegex("ssv/.*", "debug") // for debugging

	scenariosToRun := []*scenarios.IntegrationTest{
		scenarios.Regular(types.BNRoleAttester), // TODO: test other roles
	}

	for _, scenario := range scenariosToRun {
		require.NoError(t, scenario.Run())
	}
}
