package validation

import (
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/message"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// validateSlotTime's early bound is clockErrorTolerance plus earlyMessageMargin for every role (issue
// #3026), plus the proposer-lookahead window for proposer preferences alone.
func TestValidateSlotTime_Earliness(t *testing.T) {
	netCfg := networkconfig.TestNetwork
	mv := &messageValidator{netCfg: netCfg}

	slot := phase0.Slot(1000)
	margin := clockErrorTolerance + earlyMessageMargin
	lookahead := time.Duration(proposerPreferencesEarlyEpochs*netCfg.SlotsPerEpoch) * netCfg.SlotDuration

	roles := []spectypes.RunnerRole{
		spectypes.RoleCommittee, spectypes.RoleAggregatorCommittee, spectypes.RoleProposer, ssvtypes.RoleAggregator,
		ssvtypes.RoleSyncCommitteeContribution, spectypes.RoleValidatorRegistration, spectypes.RoleVoluntaryExit,
		spectypes.RolePTCAttester,
	}
	for _, role := range roles {
		t.Run(message.RunnerRoleToString(role), func(t *testing.T) {
			require.NoError(t, mv.validateSlotTime(slot, role, netCfg.SlotStartTime(slot).Add(-margin)), "at the margin")
			err := mv.validateSlotTime(slot, role, netCfg.SlotStartTime(slot).Add(-margin-time.Millisecond))
			require.ErrorIs(t, err, ErrEarlySlotMessage, "beyond the margin")
		})
	}

	t.Run("proposer preferences", func(t *testing.T) {
		role := spectypes.RoleProposerPreferences
		require.NoError(t, mv.validateSlotTime(slot, role, netCfg.SlotStartTime(slot).Add(-margin-lookahead)), "at the lookahead window")
		err := mv.validateSlotTime(slot, role, netCfg.SlotStartTime(slot).Add(-margin-lookahead-time.Millisecond))
		require.ErrorIs(t, err, ErrEarlySlotMessage, "beyond the lookahead window")
	})
}

// TestCommitteeRole locks the publish/receive symmetry for committee-backed roles.
// Every role that p2pNetwork.BroadcastAtSlot routes to the committee topic
// (RoleCommittee, RoleAggregatorCommittee) must resolve to the committee lookup on
// receive, otherwise those messages would be rejected as unknown validators.
func TestCommitteeRole(t *testing.T) {
	mv := &messageValidator{}

	require.True(t, mv.committeeRole(spectypes.RoleCommittee))
	require.True(t, mv.committeeRole(spectypes.RoleAggregatorCommittee))

	require.False(t, mv.committeeRole(spectypes.RoleProposer))
	require.False(t, mv.committeeRole(ssvtypes.RoleAggregator))
}
