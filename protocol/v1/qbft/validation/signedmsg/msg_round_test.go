package signedmsg

import (
	"testing"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"github.com/stretchr/testify/require"
)

func TestMsgRound(t *testing.T) {
	tests := []struct {
		name          string
		expectedRound specqbft.Round
		actualRound   specqbft.Round
		expectedError string
	}{
		{
			"valid",
			1,
			1,
			"",
		},
		{
			"different msg rounds",
			1,
			2,
			"message round (2) does not equal state round (1)",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pipeline := ValidateRound(test.expectedRound)
			err := pipeline.Run(&specqbft.SignedMessage{
				Message: &specqbft.Message{
					Round: test.actualRound,
				},
			})

			if len(test.expectedError) == 0 {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, test.expectedError)
			}
		})
	}
}
