package auth

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestMsgSeq(t *testing.T) {
	tests := []struct {
		name          string
		expectedSeq   uint64
		actualSeq     uint64
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
			"invalid message sequence number",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pipeline := ValidateSequenceNumber(test.expectedSeq)
			err := pipeline.Run(&proto.SignedMessage{
				Message: &proto.Message{
					SeqNumber: test.actualSeq,
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
