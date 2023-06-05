package validator

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_validateValidatorAddedEvent(t *testing.T) {
	generateOperators := func(count uint64) []uint64 {
		result := make([]uint64, 0)
		for i := uint64(1); i <= count; i++ {
			result = append(result, i)
		}
		return result
	}

	tt := []struct {
		name      string
		operators []uint64
		err       string
	}{
		{
			name:      "4 operators",
			operators: generateOperators(4),
			err:       "",
		},
		{
			name:      "7 operators",
			operators: generateOperators(7),
			err:       "",
		},
		{
			name:      "10 operators",
			operators: generateOperators(10),
			err:       "",
		},
		{
			name:      "13 operators",
			operators: generateOperators(13),
			err:       "",
		},
		{
			name:      "14 operators",
			operators: generateOperators(14),
			err:       "too many operators (14)",
		},
		{
			name:      "0 operators",
			operators: generateOperators(0),
			err:       "no operators",
		},
		{
			name:      "1 operator",
			operators: generateOperators(1),
			err:       "given operator count (1) cannot build a 3f+1 quorum",
		},
		{
			name:      "2 operators",
			operators: generateOperators(2),
			err:       "given operator count (2) cannot build a 3f+1 quorum",
		},
		{
			name:      "3 operators",
			operators: generateOperators(3),
			err:       "given operator count (3) cannot build a 3f+1 quorum",
		},
		{
			name:      "5 operators",
			operators: generateOperators(5),
			err:       "given operator count (5) cannot build a 3f+1 quorum",
		},
		{
			name:      "duplicated operator",
			operators: []uint64{1, 2, 3, 3},
			err:       "duplicated operator ID (3)",
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := validateOperators(tc.operators)
			if tc.err == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.err)
			}
		})
	}
}
