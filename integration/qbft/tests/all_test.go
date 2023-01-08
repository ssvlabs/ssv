package tests

import (
	"github.com/bloxapp/ssv-spec/types"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/integration/qbft/scenarios"
)

func Test_Automation_QBFTScenarios(t *testing.T) {
	// _ = logging.SetLogLevelRegex("ssv/.*", "debug") // for debugging

	scenariosToRun := []*scenarios.IntegrationTest{
		scenarios.Regular(types.BNRoleAttester),
		//scenarios.Regular(types.BNRoleAggregator), //fails on OperatorID = 3
		//scenarios.Regular(types.BNRoleProposer), //fails
		//scenarios.Regular(types.BNRoleSyncCommittee),
		//scenarios.Regular(types.BNRoleSyncCommitteeContribution),
	}

	for _, scenario := range scenariosToRun {
		require.NoError(t, scenario.Run())
	}
}
