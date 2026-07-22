package utils

import (
	"testing"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

func TestFormatRunnerRole(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		role spectypes.RunnerRole
		want string
	}{
		{
			name: "deprecated aggregator role",
			role: ssvtypes.RoleAggregator,
			want: "AGGREGATOR",
		},
		{
			name: "deprecated sync committee contribution role",
			role: ssvtypes.RoleSyncCommitteeContribution,
			want: "SYNC_COMMITTEE_CONTRIBUTION",
		},
		{
			name: "committee role",
			role: spectypes.RoleCommittee,
			want: "COMMITTEE",
		},
		{
			name: "proposer role",
			role: spectypes.RoleProposer,
			want: "PROPOSER",
		},
		{
			name: "validator registration role",
			role: spectypes.RoleValidatorRegistration,
			want: "VALIDATOR_REGISTRATION",
		},
		{
			name: "voluntary exit role",
			role: spectypes.RoleVoluntaryExit,
			want: "VOLUNTARY_EXIT",
		},
		{
			name: "aggregator committee role",
			role: spectypes.RoleAggregatorCommittee,
			want: "AGGREGATOR_COMMITTEE",
		},
		{
			name: "unknown role",
			role: spectypes.RoleUnknown,
			want: "UNDEFINED",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := FormatRunnerRole(tc.role)
			require.Equal(t, tc.want, got)
		})
	}

	// The deprecated legacy roles must never collapse to "UNDEFINED", or they'd collide
	// with each other and with genuinely unknown roles.
	t.Run("legacy roles never collide with UNDEFINED", func(t *testing.T) {
		t.Parallel()

		require.NotEqual(t, "UNDEFINED", FormatRunnerRole(ssvtypes.RoleAggregator))
		require.NotEqual(t, "UNDEFINED", FormatRunnerRole(ssvtypes.RoleSyncCommitteeContribution))
		require.NotEqual(t, FormatRunnerRole(ssvtypes.RoleAggregator), FormatRunnerRole(ssvtypes.RoleSyncCommitteeContribution))
	})
}
