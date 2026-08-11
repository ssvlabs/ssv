package utils

import (
	"testing"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/message"
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

// TestRunnerRoleStringMappersLockstep guards the contract documented on
// ssvtypes.RunnerRoleToString and message.RunnerRoleToString: the two mappers are
// independent (one reaches the strings via the spec's String() plus a deprecated-role
// shim, the other via its own switch) and must produce the same string for every runner
// role that is valid in any fork. A role added or deprecated in one must be reflected in
// the other — this test is what fails when they drift.
func TestRunnerRoleStringMappersLockstep(t *testing.T) {
	t.Parallel()

	// The full role union across forks, mirroring messageValidator.validRoleUnion.
	roles := []spectypes.RunnerRole{
		spectypes.RoleCommittee,
		spectypes.RoleAggregatorCommittee,
		spectypes.RoleProposer,
		spectypes.RoleValidatorRegistration,
		spectypes.RoleVoluntaryExit,
		ssvtypes.RoleAggregator,
		ssvtypes.RoleSyncCommitteeContribution,
	}

	for _, role := range roles {
		require.Equal(t, message.RunnerRoleToString(role), FormatRunnerRole(role),
			"role %d: message.RunnerRoleToString and utils.FormatRunnerRole disagree", role)
	}

	// Sweep beyond the explicit list so a role added to the spec — which FormatRunnerRole
	// picks up automatically via (RunnerRole).String() but message.RunnerRoleToString's
	// hand-written switch would miss — fails here instead of drifting silently. Roles the
	// spec does not know return "UNDEFINED" and are skipped: divergence on genuinely
	// unknown values is intentional (the deprecated Alan roles also stringify to
	// "UNDEFINED" in the spec, but they are covered by the explicit list above).
	for i := 0; i <= 15; i++ {
		role := spectypes.RunnerRole(i)
		if role.String() == "UNDEFINED" {
			continue
		}
		require.Equal(t, message.RunnerRoleToString(role), FormatRunnerRole(role),
			"role %d is known to the spec but the two mappers disagree", role)
	}
}
