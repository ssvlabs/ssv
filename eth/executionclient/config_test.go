package executionclient

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsFinalityActive(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name              string
		currentEpoch      uint64
		finalityForkEpoch uint64
		expected          bool
	}{
		{
			name:              "finality disabled when finalityForkEpoch is 0",
			currentEpoch:      100,
			finalityForkEpoch: 0,
			expected:          false,
		},
		{
			name:              "finality inactive when current epoch is less than fork epoch",
			currentEpoch:      99,
			finalityForkEpoch: 100,
			expected:          false,
		},
		{
			name:              "finality active when current epoch equals fork epoch",
			currentEpoch:      100,
			finalityForkEpoch: 100,
			expected:          true,
		},
		{
			name:              "finality active when current epoch greater than fork epoch",
			currentEpoch:      101,
			finalityForkEpoch: 100,
			expected:          true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := IsFinalityActive(tc.currentEpoch, tc.finalityForkEpoch)
			require.Equal(t, tc.expected, result)
		})
	}
}
