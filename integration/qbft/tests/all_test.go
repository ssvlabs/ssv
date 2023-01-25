package tests

import (
	"github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/integration/qbft/scenarios"
	"github.com/stretchr/testify/require"
	"testing"
)

func Test_Integration_QBFTScenarios4Committee(t *testing.T) {
	//_ = logging.SetLogLevelRegex("ssv/.*", "debug") // for debugging TODO: ssv/.* or ssv/*. ?
	f := 1

	tests := []*scenarios.IntegrationTest{
		scenarios.RegularAttester(f),
		scenarios.RegularAggregator(),
		scenarios.RegularSyncCommittee(),
		scenarios.RegularSyncCommitteeContribution(),
		scenarios.RegularProposer(),
		scenarios.F1Decided(types.BNRoleAttester),
		scenarios.RoundChange(types.BNRoleAttester),
	}

	for _, test := range tests {
		t.Run(t.Name(), func(t *testing.T) {
			require.NoError(t, test.Run(f, []types.OperatorID{1, 2, 3, 4}))
		})
	}
}

func Test_Integration_QBFTScenarios7Committee(t *testing.T) {
	//_ = logging.SetLogLevelRegex("ssv/.*", "debug") // for debugging TODO: ssv/.* or ssv/*. ?

	f := 2

	operatorIDs := []types.OperatorID{1, 2, 3, 4, 5, 6, 7}

	require.NoError(t, scenarios.RegularAttester(f).Run(f, operatorIDs))
}

func Test_Integration_QBFTScenarios10Committee(t *testing.T) {
	//_ = logging.SetLogLevelRegex("ssv/.*", "debug") // for debugging TODO: ssv/.* or ssv/*. ?

	f := 3

	operatorIDs := []types.OperatorID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

	require.NoError(t, scenarios.RegularAttester(f).Run(f, operatorIDs))
}
