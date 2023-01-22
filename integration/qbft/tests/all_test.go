package tests

import (
	"github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/integration/qbft/scenarios"
	"github.com/stretchr/testify/require"
	"testing"
)

func Test_Integration_QBFTScenarios(t *testing.T) {
	//_ = logging.SetLogLevelRegex("ssv/.*", "debug") // for debugging TODO: ssv/.* or ssv/*. ?

	require.NoError(t, scenarios.RegularAttester(1, scenarios.GetShareSet(1)).Run(scenarios.GetShareSet(1)))
	require.NoError(t, scenarios.RegularAttester(2, scenarios.GetShareSet(2)).Run(scenarios.GetShareSet(2)))
	require.NoError(t, scenarios.RegularAttester(3, scenarios.GetShareSet(3)).Run(scenarios.GetShareSet(3)))

	require.NoError(t, scenarios.RegularAggregator().Run(scenarios.GetShareSet(1)))
	require.NoError(t, scenarios.RegularProposer().Run(scenarios.GetShareSet(1)))
	require.NoError(t, scenarios.RegularSyncCommittee().Run(scenarios.GetShareSet(1)))
	require.NoError(t, scenarios.RegularSyncCommitteeContribution().Run(scenarios.GetShareSet(1)))
	require.NoError(t, scenarios.RoundChange(types.BNRoleAttester).Run(scenarios.GetShareSet(1)))
	require.NoError(t, scenarios.F1Decided(types.BNRoleAttester).Run(scenarios.GetShareSet(1)))
}
