package tests

import (
	"testing"

	"github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	protocolstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
)

func TestRegular4CommitteeScenario(t *testing.T) {
	t.Skip("to be fixed")

	regular := &Scenario{
		Committee: 4,
		Duties: map[spectypes.OperatorID]DutyProperties{
			1: {Slot: DefaultSlot, ValidatorIndex: 1, Delay: NoDelay},
			2: {Slot: DefaultSlot, ValidatorIndex: 1, Delay: NoDelay},
			3: {Slot: DefaultSlot, ValidatorIndex: 1, Delay: NoDelay},
			4: {Slot: DefaultSlot, ValidatorIndex: 1, Delay: NoDelay},
		},
		ValidationFunctions: map[spectypes.OperatorID]func(*testing.T, int, *protocolstorage.StoredInstance){
			1: regularValidator(),
			2: regularValidator(),
			3: regularValidator(),
			4: regularValidator(),
		},
	}

	//regular.Run(t, spectypes.RoleCommittee) // fails because committee runner needs to be created
	regular.Run(t, spectypes.RoleAggregator)
	regular.Run(t, spectypes.RoleProposer)
	regular.Run(t, spectypes.RoleSyncCommitteeContribution)
}

func TestRegular7CommitteeScenario(t *testing.T) {
	t.Skip("to be fixed")

	regular := &Scenario{
		Committee: 7,
		Duties: map[spectypes.OperatorID]DutyProperties{
			1: {Slot: DefaultSlot, ValidatorIndex: 1, Delay: NoDelay},
			2: {Slot: DefaultSlot, ValidatorIndex: 1, Delay: NoDelay},
			3: {Slot: DefaultSlot, ValidatorIndex: 1, Delay: NoDelay},
			4: {Slot: DefaultSlot, ValidatorIndex: 1, Delay: NoDelay},
			5: {Slot: DefaultSlot, ValidatorIndex: 1, Delay: NoDelay},
			6: {Slot: DefaultSlot, ValidatorIndex: 1, Delay: NoDelay},
			7: {Slot: DefaultSlot, ValidatorIndex: 1, Delay: NoDelay},
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

	regular.Run(t, spectypes.RoleCommittee)
	regular.Run(t, spectypes.RoleAggregator)
	regular.Run(t, spectypes.RoleProposer)
	regular.Run(t, spectypes.RoleSyncCommitteeContribution)
}

func TestRegular10CommitteeScenario(t *testing.T) {
	t.Skip("to be fixed")

	regular := &Scenario{
		Committee: 10,
		Duties: map[spectypes.OperatorID]DutyProperties{
			1:  {Slot: DefaultSlot, ValidatorIndex: 1, Delay: NoDelay},
			2:  {Slot: DefaultSlot, ValidatorIndex: 1, Delay: NoDelay},
			3:  {Slot: DefaultSlot, ValidatorIndex: 1, Delay: NoDelay},
			4:  {Slot: DefaultSlot, ValidatorIndex: 1, Delay: NoDelay},
			5:  {Slot: DefaultSlot, ValidatorIndex: 1, Delay: NoDelay},
			6:  {Slot: DefaultSlot, ValidatorIndex: 1, Delay: NoDelay},
			7:  {Slot: DefaultSlot, ValidatorIndex: 1, Delay: NoDelay},
			8:  {Slot: DefaultSlot, ValidatorIndex: 1, Delay: NoDelay},
			9:  {Slot: DefaultSlot, ValidatorIndex: 1, Delay: NoDelay},
			10: {Slot: DefaultSlot, ValidatorIndex: 1, Delay: NoDelay},
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

	regular.Run(t, spectypes.RoleCommittee)
	regular.Run(t, spectypes.RoleAggregator)
	regular.Run(t, spectypes.RoleProposer)
	regular.Run(t, spectypes.RoleSyncCommitteeContribution)
}

func regularValidator() func(t *testing.T, committee int, actual *protocolstorage.StoredInstance) {
	return func(t *testing.T, committee int, actual *protocolstorage.StoredInstance) {
		require.EqualValues(t, DefaultSlot, actual.State.Height, "height not matching")
		require.Equal(t, int(qbft.FirstRound), int(actual.State.Round), "round not matching")

		require.NotNil(t, actual.DecidedMessage, "no decided message")
		require.Greater(t, len(actual.DecidedMessage.Signatures), quorum(committee)-1, "no commit quorum")
	}
}
