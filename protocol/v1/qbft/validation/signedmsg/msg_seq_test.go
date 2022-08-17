package signedmsg

import (
	"testing"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"github.com/stretchr/testify/require"
)

func TestMsgSeq(t *testing.T) {
	tests := []struct {
		name          string
		expectedSeq   specqbft.Height
		actualSeq     specqbft.Height
		expectedError string
	}{
		{
			"valid",
			1,
			1,
			"",
		},
		{
			"different msg seq",
			1,
			2,
			"msg Height wrong",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pipeline := ValidateSequenceNumber(test.expectedSeq)
			err := pipeline.Run(&specqbft.SignedMessage{
				Message: &specqbft.Message{
					Height: test.actualSeq,
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
