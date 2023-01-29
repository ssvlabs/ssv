package tests

import (
	"github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	protocolstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRegular4CommitteeScenario(t *testing.T) {
	regular := &Scenario{
		Committee:      4,
		ExpectedHeight: qbft.FirstHeight,
		Duties: map[spectypes.OperatorID][]DutyProperties{
			1: {{DefaultSlot, 1, NoDelay}},
			2: {{DefaultSlot, 1, NoDelay}},
			3: {{DefaultSlot, 1, NoDelay}},
			4: {{DefaultSlot, 1, NoDelay}},
		},
		ValidationFunctions: map[spectypes.OperatorID]func(*testing.T, int, *protocolstorage.StoredInstance){
			1: regularValidator(),
			2: regularValidator(),
			3: regularValidator(),
			4: regularValidator(),
		},
	}

	regular.Run(t, spectypes.BNRoleAttester)
	regular.Run(t, spectypes.BNRoleAggregator)
	regular.Run(t, spectypes.BNRoleProposer)
	regular.Run(t, spectypes.BNRoleSyncCommittee)
	regular.Run(t, spectypes.BNRoleSyncCommitteeContribution)
}

func TestRegular7CommitteeScenario(t *testing.T) {
	regular := &Scenario{
		Committee:      7,
		ExpectedHeight: qbft.FirstHeight,
		Duties: map[spectypes.OperatorID][]DutyProperties{
			1: {{DefaultSlot, 1, NoDelay}},
			2: {{DefaultSlot, 1, NoDelay}},
			3: {{DefaultSlot, 1, NoDelay}},
			4: {{DefaultSlot, 1, NoDelay}},
			5: {{DefaultSlot, 1, NoDelay}},
			6: {{DefaultSlot, 1, NoDelay}},
			7: {{DefaultSlot, 1, NoDelay}},
		},
		ValidationFunctions: map[spectypes.OperatorID]func(*testing.T, int, *protocolstorage.StoredInstance){
			1: regularValidator(),
			2: regularValidator(),
			3: regularValidator(),
			4: regularValidator(),
			5: regularValidator(),
			6: regularValidator(),
			7: regularValidator(),
		},
	}

	regular.Run(t, spectypes.BNRoleAttester)
	regular.Run(t, spectypes.BNRoleAggregator)
	regular.Run(t, spectypes.BNRoleProposer)
	regular.Run(t, spectypes.BNRoleSyncCommittee)
	regular.Run(t, spectypes.BNRoleSyncCommitteeContribution)
}

func TestRegular10CommitteeScenario(t *testing.T) {
	regular := &Scenario{
		Committee:      10,
		ExpectedHeight: qbft.FirstHeight,
		Duties: map[spectypes.OperatorID][]DutyProperties{
			1:  {{DefaultSlot, 1, NoDelay}},
			2:  {{DefaultSlot, 1, NoDelay}},
			3:  {{DefaultSlot, 1, NoDelay}},
			4:  {{DefaultSlot, 1, NoDelay}},
			5:  {{DefaultSlot, 1, NoDelay}},
			6:  {{DefaultSlot, 1, NoDelay}},
			7:  {{DefaultSlot, 1, NoDelay}},
			8:  {{DefaultSlot, 1, NoDelay}},
			9:  {{DefaultSlot, 1, NoDelay}},
			10: {{DefaultSlot, 1, NoDelay}},
		},
		ValidationFunctions: map[spectypes.OperatorID]func(*testing.T, int, *protocolstorage.StoredInstance){
			1:  regularValidator(),
			2:  regularValidator(),
			3:  regularValidator(),
			4:  regularValidator(),
			5:  regularValidator(),
			6:  regularValidator(),
			7:  regularValidator(),
			8:  regularValidator(),
			9:  regularValidator(),
			10: regularValidator(),
		},
	}

	regular.Run(t, spectypes.BNRoleAttester)
	regular.Run(t, spectypes.BNRoleAggregator)
	regular.Run(t, spectypes.BNRoleProposer)
	regular.Run(t, spectypes.BNRoleSyncCommittee)
	regular.Run(t, spectypes.BNRoleSyncCommitteeContribution)
}

func regularValidator() func(t *testing.T, committee int, actual *protocolstorage.StoredInstance) {
	return func(t *testing.T, committee int, actual *protocolstorage.StoredInstance) {
		require.Equal(t, qbft.FirstHeight, actual.State.Height, "height not matching")
		require.Equal(t, qbft.FirstRound, actual.State.Round, "round not matching")

		require.NotNil(t, actual.DecidedMessage, "no decided message")
		if quorum(committee) > len(actual.DecidedMessage.Signers) {
			require.Fail(t, "no commit qourum")
		}
	}
}
