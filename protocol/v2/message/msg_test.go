package message

import (
	"testing"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

func TestBeaconRoleFromString(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected spectypes.BeaconRole
		hasError bool
	}{
		{name: "attester", input: "ATTESTER", expected: spectypes.BNRoleAttester},
		{name: "aggregator", input: "AGGREGATOR", expected: spectypes.BNRoleAggregator},
		{name: "proposer", input: "PROPOSER", expected: spectypes.BNRoleProposer},
		{name: "sync committee", input: "SYNC_COMMITTEE", expected: spectypes.BNRoleSyncCommittee},
		{name: "sync committee contribution", input: "SYNC_COMMITTEE_CONTRIBUTION", expected: spectypes.BNRoleSyncCommitteeContribution},
		{name: "validator registration", input: "VALIDATOR_REGISTRATION", expected: spectypes.BNRoleValidatorRegistration},
		{name: "voluntary exit", input: "VOLUNTARY_EXIT", expected: spectypes.BNRoleVoluntaryExit},
		{name: "unknown role errors", input: "COMMITTEE", hasError: true},
		{name: "empty string errors", input: "", hasError: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			role, err := BeaconRoleFromString(tc.input)
			if tc.hasError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expected, role)
		})
	}
}

func TestRunnerRoleFromString(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected spectypes.RunnerRole
		hasError bool
	}{
		{name: "committee", input: "COMMITTEE", expected: spectypes.RoleCommittee},
		{name: "aggregator committee", input: "AGGREGATOR_COMMITTEE", expected: spectypes.RoleAggregatorCommittee},
		{name: "aggregator", input: "AGGREGATOR", expected: ssvtypes.RoleAggregator},
		{name: "proposer", input: "PROPOSER", expected: spectypes.RoleProposer},
		{name: "sync committee contribution", input: "SYNC_COMMITTEE_CONTRIBUTION", expected: ssvtypes.RoleSyncCommitteeContribution},
		{name: "validator registration", input: "VALIDATOR_REGISTRATION", expected: spectypes.RoleValidatorRegistration},
		{name: "voluntary exit", input: "VOLUNTARY_EXIT", expected: spectypes.RoleVoluntaryExit},
		{name: "sync committee (deprecated bare role) errors", input: "SYNC_COMMITTEE", hasError: true},
		{name: "unknown role errors", input: "NOT_A_ROLE", hasError: true},
		{name: "empty string errors", input: "", hasError: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			role, err := RunnerRoleFromString(tc.input)
			if tc.hasError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expected, role)
		})
	}
}

func TestRunnerRoleToString(t *testing.T) {
	testCases := []struct {
		name     string
		role     spectypes.RunnerRole
		expected string
	}{
		{name: "committee", role: spectypes.RoleCommittee, expected: "COMMITTEE"},
		{name: "aggregator committee", role: spectypes.RoleAggregatorCommittee, expected: "AGGREGATOR_COMMITTEE"},
		{name: "aggregator", role: ssvtypes.RoleAggregator, expected: "AGGREGATOR"},
		{name: "proposer", role: spectypes.RoleProposer, expected: "PROPOSER"},
		{name: "sync committee contribution", role: ssvtypes.RoleSyncCommitteeContribution, expected: "SYNC_COMMITTEE_CONTRIBUTION"},
		{name: "validator registration", role: spectypes.RoleValidatorRegistration, expected: "VALIDATOR_REGISTRATION"},
		{name: "voluntary exit", role: spectypes.RoleVoluntaryExit, expected: "VOLUNTARY_EXIT"},
		{name: "unknown role", role: spectypes.RunnerRole(999), expected: "unknown(999)"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, RunnerRoleToString(tc.role))
		})
	}
}

// TestRunnerRoleFromString_ToString_RoundTrip guards against the two functions drifting
// apart (e.g. a new role added to one but not the other).
func TestRunnerRoleFromString_ToString_RoundTrip(t *testing.T) {
	roles := []spectypes.RunnerRole{
		spectypes.RoleCommittee,
		spectypes.RoleAggregatorCommittee,
		ssvtypes.RoleAggregator,
		spectypes.RoleProposer,
		ssvtypes.RoleSyncCommitteeContribution,
		spectypes.RoleValidatorRegistration,
		spectypes.RoleVoluntaryExit,
	}

	for _, role := range roles {
		s := RunnerRoleToString(role)
		got, err := RunnerRoleFromString(s)
		require.NoError(t, err, "round-trip failed for role %v (string %q)", role, s)
		assert.Equal(t, role, got)
	}
}
