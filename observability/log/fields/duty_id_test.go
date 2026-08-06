package fields

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

func TestBuildDutyID(t *testing.T) {
	t.Parallel()

	got := BuildDutyID(phase0.Epoch(3), phase0.Slot(100), spectypes.RoleCommittee, phase0.ValidatorIndex(42))
	require.Equal(t, "COMMITTEE-e3-s100-v42", got)
}

func TestBuildDutyID_PreForkLegacyRoles(t *testing.T) {
	t.Parallel()

	// Pre-fork aggregator and sync committee contribution duties must render distinct,
	// human-readable duty IDs rather than colliding on "UNDEFINED".
	aggregatorID := BuildDutyID(phase0.Epoch(3), phase0.Slot(100), ssvtypes.RoleAggregator, phase0.ValidatorIndex(42))
	require.Equal(t, "AGGREGATOR-e3-s100-v42", aggregatorID)

	syncCommitteeContributionID := BuildDutyID(phase0.Epoch(3), phase0.Slot(100), ssvtypes.RoleSyncCommitteeContribution, phase0.ValidatorIndex(42))
	require.Equal(t, "SYNC_COMMITTEE_CONTRIBUTION-e3-s100-v42", syncCommitteeContributionID)

	require.NotEqual(t, aggregatorID, syncCommitteeContributionID)
}

func TestBuildCommitteeDutyID(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		operators []spectypes.OperatorID
		epoch     phase0.Epoch
		slot      phase0.Slot
		role      spectypes.RunnerRole
		want      string
	}{
		{
			name:      "committee role",
			operators: []spectypes.OperatorID{1, 2, 3},
			epoch:     phase0.Epoch(5),
			slot:      phase0.Slot(160),
			role:      spectypes.RoleCommittee,
			want:      "COMMITTEE-1_2_3-e5-s160",
		},
		{
			name:      "aggregator-committee role produces a distinct duty ID for the same slot/operators",
			operators: []spectypes.OperatorID{1, 2, 3},
			epoch:     phase0.Epoch(5),
			slot:      phase0.Slot(160),
			role:      spectypes.RoleAggregatorCommittee,
			want:      "AGGREGATOR_COMMITTEE-1_2_3-e5-s160",
		},
		{
			name:      "empty operator list",
			operators: nil,
			epoch:     phase0.Epoch(0),
			slot:      phase0.Slot(0),
			role:      spectypes.RoleCommittee,
			want:      "COMMITTEE--e0-s0",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := BuildCommitteeDutyID(tc.operators, tc.epoch, tc.slot, tc.role)
			require.Equal(t, tc.want, got)
		})
	}

	// The role must be reflected in the ID so committee and aggregator-committee duties for the
	// same committee/slot never collide.
	t.Run("committee and aggregator-committee IDs never collide", func(t *testing.T) {
		t.Parallel()

		operators := []spectypes.OperatorID{7, 8}
		committeeID := BuildCommitteeDutyID(operators, 1, 1, spectypes.RoleCommittee)
		aggregatorID := BuildCommitteeDutyID(operators, 1, 1, spectypes.RoleAggregatorCommittee)
		require.NotEqual(t, committeeID, aggregatorID)
	})
}
