package deterministic

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeterministic_Bump(t *testing.T) {
	d, err := New([]byte{1, 1, 1, 1, 1, 1, 1, 1}, 4)
	require.NoError(t, err)
	for i := 1; i < 50; i++ {
		t.Run(fmt.Sprintf("bump %d", i), func(t *testing.T) {
			require.EqualValues(t, uint64(i%4), d.Calculate(uint64(i)))
		})
	}
}

func TestDeterministic_SetSeed(t *testing.T) {
	tests := []struct {
		name           string
		seed           []byte
		committeeSize  uint64
		expectedLeader uint64
		expectedErr    string
	}{
		{
			"valid",
			[]byte{1, 1, 1, 1, 1, 1, 1, 1},
			10,
			4,
			"",
		},
		{
			"nil seed",
			nil,
			10,
			4,
			"input seed can't be nil or of length 0",
		},
		{
			"zero length seed",
			[]byte{},
			10,
			4,
			"input seed can't be nil or of length 0",
		},
		{
			"valid",
			[]byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
			10,
			9,
			"",
		},
		{
			"valid",
			[]byte{1, 1, 1, 1, 1, 1, 1, 2},
			10,
			4,
			"",
		},
		{
			"valid",
			[]byte{1, 2, 3, 4, 5, 6, 7, 8},
			4,
			2,
			"",
		},
		{
			"valid",
			[]byte{1, 2, 3, 4, 5, 6, 11, 12},
			4,
			3,
			"",
		},
		{
			"valid",
			[]byte{1, 2, 3, 22, 5, 6, 11, 12},
			4,
			2,
			"",
		},
		{
			"valid",
			[]byte{1, 2, 0, 0, 0, 0, 0, 0},
			4,
			1,
			"",
		},
		{
			"valid",
			[]byte{1, 0, 0, 0, 0, 0, 0, 0},
			4,
			0,
			"",
		},
		{
			"valid",
			[]byte{0, 0, 0, 0, 0, 0, 0, 1},
			4,
			1,
			"",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			d, err := New(test.seed, test.committeeSize)
			if len(test.expectedErr) > 0 {
				require.EqualError(t, err, test.expectedErr)
			} else {
				require.NoError(t, err)
				require.EqualValues(t, test.expectedLeader, d.Calculate(test.committeeSize))
			}

		})
	}
}
