package validator

import (
	"testing"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

func Test_mergeQuorums(t *testing.T) {
	tests := []struct {
		name     string
		quorum1  []spectypes.OperatorID
		quorum2  []spectypes.OperatorID
		expected []spectypes.OperatorID
	}{
		{
			name:     "Both quorums empty",
			quorum1:  []spectypes.OperatorID{},
			quorum2:  []spectypes.OperatorID{},
			expected: []spectypes.OperatorID{},
		},
		{
			name:     "First quorum empty",
			quorum1:  []spectypes.OperatorID{},
			quorum2:  []spectypes.OperatorID{1, 2, 3},
			expected: []spectypes.OperatorID{1, 2, 3},
		},
		{
			name:     "Second quorum empty",
			quorum1:  []spectypes.OperatorID{1, 2, 3},
			quorum2:  []spectypes.OperatorID{},
			expected: []spectypes.OperatorID{1, 2, 3},
		},
		{
			name:     "No duplicates",
			quorum1:  []spectypes.OperatorID{1, 3, 5},
			quorum2:  []spectypes.OperatorID{2, 4, 6},
			expected: []spectypes.OperatorID{1, 2, 3, 4, 5, 6},
		},
		{
			name:     "With duplicates",
			quorum1:  []spectypes.OperatorID{1, 2, 3, 5},
			quorum2:  []spectypes.OperatorID{3, 4, 5, 6},
			expected: []spectypes.OperatorID{1, 2, 3, 4, 5, 6},
		},
		{
			name:     "All duplicates",
			quorum1:  []spectypes.OperatorID{1, 2, 3},
			quorum2:  []spectypes.OperatorID{1, 2, 3},
			expected: []spectypes.OperatorID{1, 2, 3},
		},
		{
			name:     "Unsorted input quorums",
			quorum1:  []spectypes.OperatorID{5, 1, 3},
			quorum2:  []spectypes.OperatorID{4, 2, 6},
			expected: []spectypes.OperatorID{1, 2, 3, 4, 5, 6},
		},
		{
			name:     "Large quorum size",
			quorum1:  []spectypes.OperatorID{1, 3, 5, 7, 9, 11, 13},
			quorum2:  []spectypes.OperatorID{2, 4, 6, 8, 10, 12, 14},
			expected: []spectypes.OperatorID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeQuorums(tt.quorum1, tt.quorum2)
			require.Equal(t, tt.expected, result)
		})
	}
}
