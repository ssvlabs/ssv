package tests

import (
	"testing"
	"time"

	"github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	protocolstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
)

func TestRoundChange4CommitteeScenario(t *testing.T) {
	t.Skip("to be fixed")

	t.SkipNow() // TODO: test is flakey

	roundChange := &Scenario{
		Committee: 4,
		Duties: map[spectypes.OperatorID]DutyProperties{
			2: {Slot: DefaultSlot, ValidatorIndex: 1, Delay: NoDelay},
			1: {Slot: DefaultSlot, ValidatorIndex: 1, Delay: NoDelay},
			3: {Slot: DefaultSlot, ValidatorIndex: 1, Delay: 2 * time.Second},
			4: {Slot: DefaultSlot, ValidatorIndex: 1, Delay: 2 * time.Second},
		},
		ValidationFunctions: map[spectypes.OperatorID]func(*testing.T, int, *protocolstorage.StoredInstance){
			1: roundChangeValidator(),
			2: roundChangeValidator(),
			3: roundChangeValidator(),
			4: roundChangeValidator(),
		},
	}

	roundChange.Run(t, spectypes.RoleCommittee)
	//roundChange.Run(t, spectypes.BNRoleAggregator) todo implement aggregator role support
	//roundChange.Run(t, spectypes.BNRoleProposer) todo implement proposer role support
	//roundChange.Run(t, spectypes.BNRoleSyncCommitteeContribution) todo implement sync committee contribution role support
}

// nolint: unused
func roundChangeValidator() func(t *testing.T, committee int, actual *protocolstorage.StoredInstance) {
	return func(t *testing.T, committee int, actual *protocolstorage.StoredInstance) {
		require.Equal(t, int(qbft.FirstHeight), int(actual.State.Height), "height not matching") //int conversion needs to show correct output from require
		require.Equal(t, int(qbft.Round(2)), int(actual.State.Round), "round not matching")

		require.NotNil(t, actual.DecidedMessage, "no decided message")
		require.Greater(t, len(actual.DecidedMessage.Signatures), quorum(committee)-1, "no commit qourum")

		require.Contains(t, actual.State.ProposeContainer.Msgs, qbft.Round(2), "no propose messages for round 2") // TODO: all tests fail on this assertion
		require.Len(t, actual.State.ProposeContainer.Msgs[qbft.Round(2)], 1, "propose container for round 2 contains more/less than 1 messages")
		require.Len(t, actual.State.ProposeContainer.Msgs[qbft.Round(2)][0].SignedMessage.Signatures, 1, "first message in propose container for round 2 contains more/less than 1 signer")
		require.Equal(t, int(spectypes.OperatorID(2)), int(actual.State.ProposeContainer.Msgs[qbft.Round(2)][0].SignedMessage.OperatorIDs[0]), "on second round proposer is not 2")
	}
}

// TODO: implement scenario when we have prepare quorum, but don't have commit quorum and reach timeout. in that case round shall change, but proposer remains same
