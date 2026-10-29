package validation

import (
	"testing"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/protocol/v2/message"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/protocol/v2/types/ssvtestingutils"
)

// validateSlotTime's early bound is clockErrorTolerance plus earlyMessageMargin before the moment a message
// for the slot is expected: the slot's start for every role (issue #3026), and for proposer preferences the
// start of the epoch before the slot's — the proposer lookahead, the current epoch and the next.
func TestValidateSlotTime_Earliness(t *testing.T) {
	netCfg := networkconfig.TestNetwork
	mv := &messageValidator{netCfg: netCfg}

	slot := phase0.Slot(1000)
	margin := clockErrorTolerance + earlyMessageMargin

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
		// slot 1000 sits in epoch 31; it is expected from the start of epoch 30 (slot 960) on.
		expectedFrom := netCfg.SlotStartTime(netCfg.FirstSlotAtEpoch(netCfg.EstimatedEpochAtSlot(slot) - 1))
		require.NoError(t, mv.validateSlotTime(slot, role, expectedFrom.Add(-margin)), "at the lookahead window")
		require.NoError(t, mv.validateSlotTime(slot, role, expectedFrom.Add(netCfg.SlotDuration)), "inside the window")
		err := mv.validateSlotTime(slot, role, expectedFrom.Add(-margin-time.Millisecond))
		require.ErrorIs(t, err, ErrEarlySlotMessage, "beyond the lookahead window")

		// From the first slot of epoch 30, the last slot of epoch 31 is in reach and the first of epoch 32 is
		// not — the lookahead is two epochs, not two epochs' worth of slots from the message.
		now := netCfg.SlotStartTime(netCfg.FirstSlotAtEpoch(30))
		require.NoError(t, mv.validateSlotTime(netCfg.FirstSlotAtEpoch(32)-1, role, now), "last slot of the next epoch")
		require.ErrorIs(t, mv.validateSlotTime(netCfg.FirstSlotAtEpoch(32), role, now), ErrEarlySlotMessage, "first slot of the epoch after")
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

// syncPeriodBoundaryValidator returns a validator whose duty store holds one outgoing-only sync committee member
// in period 1 and one incoming-only member in period 2, along with period 1's last slot.
func syncPeriodBoundaryValidator() (mv *messageValidator, lastSlot phase0.Slot, outgoing, incoming phase0.ValidatorIndex) {
	netCfg := networkconfig.TestNetwork
	const period = uint64(1)
	outgoing, incoming = 1, 2

	ds := dutystore.New()
	ds.SyncCommittee.Set(period, []dutystore.StoreSyncCommitteeDuty{
		{ValidatorIndex: outgoing, Duty: &eth2apiv1.SyncCommitteeDuty{ValidatorIndex: outgoing}},
	})
	ds.SyncCommittee.Set(period+1, []dutystore.StoreSyncCommitteeDuty{
		{ValidatorIndex: incoming, Duty: &eth2apiv1.SyncCommitteeDuty{ValidatorIndex: incoming}},
	})

	lastSlot = netCfg.FirstSlotAtEpoch(netCfg.FirstEpochOfSyncPeriod(period+1)) - 1
	return &messageValidator{netCfg: netCfg, dutyStore: ds}, lastSlot, outgoing, incoming
}

// In a period's last slot, contributions from both the incoming committee (on duty) and the outgoing one (still run
// there by peers on older versions) are valid.
func TestValidateBeaconDuty_SyncCommitteeContributionPeriodBoundary(t *testing.T) {
	mv, lastSlot, outgoing, incoming := syncPeriodBoundaryValidator()

	validate := func(slot phase0.Slot, validatorIndex phase0.ValidatorIndex) error {
		return mv.validateBeaconDuty(ssvtypes.RoleSyncCommitteeContribution, slot, []phase0.ValidatorIndex{validatorIndex}, false)
	}

	require.NoError(t, validate(lastSlot-1, outgoing))
	require.ErrorIs(t, validate(lastSlot-1, incoming), ErrNoDuty)

	require.NoError(t, validate(lastSlot, incoming))
	require.NoError(t, validate(lastSlot, outgoing))

	require.NoError(t, validate(lastSlot+1, incoming))
	require.ErrorIs(t, validate(lastSlot+1, outgoing), ErrNoDuty)
}

// A sync committee member lifts a small committee's per-epoch duty limit. In a period's last slot that holds for both
// the outgoing committee (on duty earlier in that epoch) and the incoming one (on duty in that slot).
func TestDutyLimit_CommitteeSyncPeriodBoundary(t *testing.T) {
	mv, lastSlot, outgoing, incoming := syncPeriodBoundaryValidator()
	msgID := ssvtestingutils.NewMsgID(spectypes.DomainType{}, make([]byte, 32), spectypes.RoleCommittee)

	limit := func(slot phase0.Slot, validatorIndex phase0.ValidatorIndex) uint64 {
		limit, ok := mv.dutyLimit(msgID, slot, []phase0.ValidatorIndex{validatorIndex})
		require.True(t, ok)
		return limit
	}
	allSlots := mv.netCfg.SlotsPerEpoch
	const attestationOnly = uint64(2) // 2*V, for a single validator

	require.Equal(t, allSlots, limit(lastSlot-1, outgoing))
	require.Equal(t, attestationOnly, limit(lastSlot-1, incoming))

	require.Equal(t, allSlots, limit(lastSlot, outgoing))
	require.Equal(t, allSlots, limit(lastSlot, incoming))

	require.Equal(t, attestationOnly, limit(lastSlot+1, outgoing))
	require.Equal(t, allSlots, limit(lastSlot+1, incoming))
}
