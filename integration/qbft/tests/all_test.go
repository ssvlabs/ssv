package tests

import (
	"github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/integration/qbft/scenarios"
	"github.com/stretchr/testify/require"
	"testing"
)

func Test_Integration_QBFTScenarios(t *testing.T) {
	//_ = logging.SetLogLevelRegex("ssv/.*", "debug") // for debugging TODO: ssv/.* or ssv/*. ?

	tests := []*scenarios.IntegrationTest{
		scenarios.RegularAggregator(),
		scenarios.RegularSyncCommittee(),
		scenarios.RoundChange(types.BNRoleAttester),
		scenarios.RegularAttester(1),
		scenarios.RegularAttester(2),
		scenarios.RegularAttester(3),
		scenarios.RegularSyncCommitteeContribution(),
		scenarios.F1Decided(types.BNRoleAttester),
		scenarios.RegularProposer(),
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			require.NoError(t, test.Run())
		})
	}
}
