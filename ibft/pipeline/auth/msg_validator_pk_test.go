package auth

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestMsgValidatorPK(t *testing.T) {
	tests := []struct {
		name          string
		expectedPK    []byte
		actualPK      []byte
		expectedError string
	}{
		{
			"valid",
			_byteArray("86b78e9d24f3efacbb3ca5958b39cdcb9b3e97d241e91c903f71392e1e4f5d7706a6c8e731e76d4e0e2ac52ccd35fcb9"),
			_byteArray("86b78e9d24f3efacbb3ca5958b39cdcb9b3e97d241e91c903f71392e1e4f5d7706a6c8e731e76d4e0e2ac52ccd35fcb9"),
			"",
		},
		{
			"invalid(not same as state PK)",
			_byteArray("86b78e9d24f3efacbb3ca5958b39cdcb9b3e97d241e91c903f71392e1e4f5d7706a6c8e731e76d4e0e2ac52ccd35fcb9"),
			_byteArray("86b78e9d24f3efacbb3ca5958b39cdcb9b3e97d241e91c903f71392e1e4f5d7706a6c8e731e76d4e0e2ac52ccd35fcb8"),
			"invalid message validator PK",
		},
		{
			"pk too short",
			_byteArray("86b78e9d24f3efacbb3ca5958b39cdcb9b3e97d241e91c903f71392e1e4f5d7706a6c8e731e76d4e0e2ac52ccd35fcb9"),
			_byteArray("86b78e9d24f3efacbb3ca5958b39cdcb9b3e97d241e91c903f71392e1e4f5d7706a6c8e731e76d4e0e2ac52ccd35fc"),
			"invalid message validator PK",
		},
		{
			"pk too long",
			_byteArray("86b78e9d24f3efacbb3ca5958b39cdcb9b3e97d241e91c903f71392e1e4f5d7706a6c8e731e76d4e0e2ac52ccd35fcb9"),
			_byteArray("86b78e9d24f3efacbb3ca5958b39cdcb9b3e97d241e91c903f71392e1e4f5d7706a6c8e731e76d4e0e2ac52ccd35fcb9b9"),
			"invalid message validator PK",
		},
		{
			"pk nil",
			_byteArray("86b78e9d24f3efacbb3ca5958b39cdcb9b3e97d241e91c903f71392e1e4f5d7706a6c8e731e76d4e0e2ac52ccd35fcb9"),
			nil,
			"invalid message validator PK",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pipeline := ValidatePKs(test.expectedPK)
			err := pipeline.Run(&proto.SignedMessage{
				Message: &proto.Message{
					ValidatorPk: test.actualPK,
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
