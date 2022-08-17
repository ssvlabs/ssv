package signedmsg

import (
	"testing"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"github.com/stretchr/testify/require"
)

func TestMsgIdentifier(t *testing.T) {
	tests := []struct {
		name               string
		expectedIdentifier []byte
		actualIdentifier   []byte
		expectedError      string
	}{
		{
			"valid",
			[]byte{1, 2, 3, 4},
			[]byte{1, 2, 3, 4},
			"",
		},
		{
			"different msg identifier",
			[]byte{1, 2, 3, 4},
			[]byte{1, 2, 3, 3},
			"message identifier (01020303) does not equal expected identifier (01020304)",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pipeline := ValidateIdentifiers(test.expectedIdentifier)
			err := pipeline.Run(&specqbft.SignedMessage{
				Message: &specqbft.Message{
					Identifier: test.actualIdentifier,
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
