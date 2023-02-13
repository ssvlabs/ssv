package tests

import (
	"github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	protocolstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRoundChange4CommitteeScenario(t *testing.T) {
	roundChange := &Scenario{
		Committee: 4,
		Duties: map[spectypes.OperatorID]DutyProperties{
			1: {DefaultSlot, 1, NoDelay},
			2: {DefaultSlot, 1, NoDelay},
			3: {DefaultSlot, 1, OneRoundDelay},
			4: {DefaultSlot, 1, OneRoundDelay},
		},
		ValidationFunctions: map[spectypes.OperatorID]func(*testing.T, int, *protocolstorage.StoredInstance){
			1: roundChangeValidator(),
			2: roundChangeValidator(),
			3: roundChangeValidator(),
			4: roundChangeValidator(),
		},
	}

	roundChange.Run(t, spectypes.BNRoleAttester)
	//roundChange.Run(t, spectypes.BNRoleAggregator)
	//roundChange.Run(t, spectypes.BNRoleProposer)
	roundChange.Run(t, spectypes.BNRoleSyncCommittee)
	//roundChange.Run(t, spectypes.BNRoleSyncCommitteeContribution)
}

func roundChangeValidator() func(t *testing.T, committee int, actual *protocolstorage.StoredInstance) {
	return func(t *testing.T, committee int, actual *protocolstorage.StoredInstance) {
		require.Equal(t, int(qbft.FirstHeight), int(actual.State.Height), "height not matching") //int conversion needs to show correct output from require
		require.Equal(t, int(qbft.Round(2)), int(actual.State.Round), "round not matching")

		require.NotNil(t, actual.DecidedMessage, "no decided message")
		if quorum(committee) > len(actual.DecidedMessage.Signers) {
			require.Fail(t, "no commit qourum")
		}

		require.Equal(t, int(spectypes.OperatorID(2)), int(actual.State.ProposeContainer.Msgs[2][0].Signers[0])) //on second round proposer is 2,
	}
}

// TODO: implement scenario when we have prepare quorum, but don't have commit quorum and reach timeout. in that case round shall change, but proposer remains same
