package tests

import (
	"github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/integration/qbft/scenarios"
	"github.com/stretchr/testify/require"
	"testing"
)

func Test_Integration_QBFTScenarios(t *testing.T) {
	//_ = logging.SetLogLevelRegex("ssv/.*", "debug") // for debugging TODO: ssv/.* or ssv/*. ?

	require.NoError(t, scenarios.RegularAttester(1).Run(1))
	require.NoError(t, scenarios.RegularAttester(2).Run(2))
	require.NoError(t, scenarios.RegularAttester(3).Run(3))

	require.NoError(t, scenarios.RegularAggregator().Run(1))
	require.NoError(t, scenarios.RegularProposer().Run(1))
	require.NoError(t, scenarios.RegularSyncCommittee().Run(1))
	require.NoError(t, scenarios.RegularSyncCommitteeContribution().Run(1))
	require.NoError(t, scenarios.RoundChange(types.BNRoleAttester).Run(1))
	require.NoError(t, scenarios.F1Decided(types.BNRoleAttester).Run(1))
}
