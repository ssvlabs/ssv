package tests

import (
	"testing"

	"github.com/bloxapp/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/integration/qbft/scenarios"
)

func Test_Integration_QBFTScenarios(t *testing.T) {
	//_ = logging.SetLogLevelRegex("ssv/.*", "debug") // for debugging TODO: ssv/.* or ssv/*. ?

	scenariosToRun := []*scenarios.IntegrationTest{
		scenarios.Regular(types.BNRoleAttester), // TODO: test other roles
		scenarios.RoundChange(types.BNRoleAttester),
	}

	for _, scenario := range scenariosToRun {
		scenario := scenario
		t.Run(scenario.Name, func(t *testing.T) {
			require.NoError(t, scenario.Run())
		})
	}
}
