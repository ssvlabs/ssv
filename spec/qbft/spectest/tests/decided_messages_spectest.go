package tests

import (
	"bytes"
	"github.com/bloxapp/ssv/spec/qbft"
	"github.com/stretchr/testify/require"
	"testing"
)

type DecidedMsgSpecTest struct {
	Name            string
	Messages        []*qbft.DecidedMessage
	EncodedMessages [][]byte
	ExpectedError   string
}

func (test *DecidedMsgSpecTest) Run(t *testing.T) {
	var lastErr error

	for i, byts := range test.EncodedMessages {
		m := &qbft.DecidedMessage{}
		if err := m.Decode(byts); err != nil {
			lastErr = err
		}

		if len(test.Messages) > 0 {
			b, err := test.Messages[i].Encode()
			if err != nil {
				lastErr = err
			}
			if !bytes.Equal(b, byts) {
				t.Fail()
			}
		}
	}

	if len(test.ExpectedError) != 0 {
		require.EqualError(t, lastErr, test.ExpectedError)
	} else {
		require.NoError(t, lastErr)
	}
}

func (test *DecidedMsgSpecTest) TestName() string {
	return test.Name
}
