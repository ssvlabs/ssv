package signedmsg

import (
	"testing"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"github.com/stretchr/testify/require"
)

func TestMsgLambda(t *testing.T) {
	tests := []struct {
		name           string
		expectedLambda []byte
		actualLambda   []byte
		expectedError  string
	}{
		{
			"valid",
			[]byte{1, 2, 3, 4},
			[]byte{1, 2, 3, 4},
			"",
		},
		{
			"different msg lambda",
			[]byte{1, 2, 3, 4},
			[]byte{1, 2, 3, 3},
			"message Lambda (01020303) does not equal expected Lambda (01020304)",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pipeline := ValidateLambdas(test.expectedLambda)
			err := pipeline.Run(&specqbft.SignedMessage{
				Message: &specqbft.Message{
					Identifier: test.actualLambda,
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
