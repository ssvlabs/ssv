package tests

import (
	"github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	protocolstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestF1DecidedScenario(t *testing.T) {
	f1DecidedScenario := &Scenario{
		Committee:      3,
		ExpectedHeight: 8,
		InitialInstances: map[spectypes.OperatorID]*StoredInstanceProperties{
			1: {7},
			2: {7},
			3: {2},
		},
		Duties: map[spectypes.OperatorID][]DutyProperties{
			1: {DutyProperties{DefaultSlot, 1, NoDelay}},
			2: {DutyProperties{DefaultSlot, 1, NoDelay}},
			3: {DutyProperties{DefaultSlot, 1, AfterOneRoundDelay}},
		},
		ValidationFunctions: map[spectypes.OperatorID]func(*testing.T, int, *protocolstorage.StoredInstance){
			1: f1DecidedValidator(8),
			2: f1DecidedValidator(8),
			3: f1DecidedValidator(8),
		},
	}

	f1DecidedScenario.Run(t, spectypes.BNRoleAttester)
	//f1DecidedScenario.Run(t, spectypes.BNRoleAggregator)
	//f1DecidedScenario.Run(t, spectypes.BNRoleProposer)
	//f1DecidedScenario.Run(t, spectypes.BNRoleSyncCommittee)
	//f1DecidedScenario.Run(t, spectypes.BNRoleSyncCommitteeContribution)
}

func f1DecidedValidator(expectedHeight qbft.Height) func(t *testing.T, committee int, actual *protocolstorage.StoredInstance) {
	return func(t *testing.T, committee int, actual *protocolstorage.StoredInstance) {
		require.Equal(t, expectedHeight, actual.State.Height, "height not matching")

		require.NotNil(t, actual.DecidedMessage, "no decided message")
		if quorum(committee) > len(actual.DecidedMessage.Signers) {
			require.Fail(t, "no commit qourum")
		}
	}
}
