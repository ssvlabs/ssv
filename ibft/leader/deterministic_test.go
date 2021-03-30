package leader

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeterministic_SetSeed(t *testing.T) {
	tests := []struct {
		name           string
		seed           []byte
		committeeSize  uint64
		expectedLeader uint64
		expectedErr    string
	}{
		{
			"valid 8 byte seed",
			[]byte{1, 1, 1, 1, 1, 1, 1, 1},
			10,
			3,
			"",
		},
		{
			"valid >8 byte seed",
			[]byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
			10,
			3,
			"",
		},
		{
			"invalid <8 byte seed",
			[]byte{1, 1, 1, 1, 1, 1, 1},
			10,
			3,
			"unexpected EOF",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			d := &Deterministic{}
			if len(test.expectedErr) > 0 {
				require.EqualError(t, d.SetSeed(test.seed, 0), test.expectedErr)
			} else {
				require.NoError(t, d.SetSeed(test.seed, 0))
				require.EqualValues(t, test.expectedLeader, d.Current(test.committeeSize))
			}

		})
	}
}
