package casts

import (
	"testing"
	"time"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDurationFromUint64 verifies that DurationFromUint64 correctly converts uint64 values to time.Duration,
// handling normal values, maximum int64 values, and overflow cases appropriately.
func TestDurationFromUint64(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    uint64
		expected time.Duration
	}{
		{
			name:     "normal duration",
			input:    1000,
			expected: time.Duration(1000),
		},
		{
			name:     "max int64",
			input:    uint64(1<<63 - 1),
			expected: time.Duration(1<<63 - 1),
		},
		{
			name:     "overflow case",
			input:    uint64(1 << 63),
			expected: time.Duration(1<<63 - 1),
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := DurationFromUint64(tt.input)

			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestBeaconRoleToRunnerRole verifies that BeaconRoleToRunnerRole correctly converts between
// various beacon roles and runner roles according to the defined mapping rules.
func TestBeaconRoleToRunnerRole(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    spectypes.BeaconRole
		expected spectypes.RunnerRole
	}{
		{
			name:     "attester role",
			input:    spectypes.BNRoleAttester,
			expected: spectypes.RoleCommittee,
		},
		{
			name:     "aggregator role",
			input:    spectypes.BNRoleAggregator,
			expected: spectypes.RoleAggregator,
		},
		{
			name:     "proposer role",
			input:    spectypes.BNRoleProposer,
			expected: spectypes.RoleProposer,
		},
		{
			name:     "sync committee role",
			input:    spectypes.BNRoleSyncCommittee,
			expected: spectypes.RoleCommittee,
		},
		{
			name:     "sync committee contribution role",
			input:    spectypes.BNRoleSyncCommitteeContribution,
			expected: spectypes.RoleSyncCommitteeContribution,
		},
		{
			name:     "validator registration role",
			input:    spectypes.BNRoleValidatorRegistration,
			expected: spectypes.RoleValidatorRegistration,
		},
		{
			name:     "voluntary exit role",
			input:    spectypes.BNRoleVoluntaryExit,
			expected: spectypes.RoleVoluntaryExit,
		},
		{
			name:     "unknown role",
			input:    spectypes.BeaconRole(999),
			expected: spectypes.RoleUnknown,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := BeaconRoleToRunnerRole(tt.input)

			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestDurationToUint64 verifies that DurationToUint64 correctly converts time.Duration to uint64,
// properly handling normal durations, zero durations, and returning errors for negative durations.
func TestDurationToUint64(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		input       time.Duration
		expected    uint64
		expectError bool
	}{
		{
			name:        "normal duration",
			input:       time.Second * 5,
			expected:    5_000_000_000,
			expectError: false,
		},
		{
			name:        "zero duration",
			input:       0,
			expected:    0,
			expectError: false,
		},
		{
			name:        "negative duration",
			input:       -time.Second,
			expected:    0,
			expectError: true,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := DurationToUint64(tt.input)

			if tt.expectError {
				require.Error(t, err)
				assert.Equal(t, ErrNegativeTime, err)
				assert.Equal(t, uint64(0), result)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}
