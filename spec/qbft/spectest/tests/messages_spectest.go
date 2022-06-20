package tests

import (
	"bytes"
	"github.com/bloxapp/ssv/spec/qbft"
	"github.com/stretchr/testify/require"
	"testing"
)

// MsgSpecTest tests encoding and decoding of a msg
type MsgSpecTest struct {
	Name            string
	Messages        []*qbft.SignedMessage
	EncodedMessages [][]byte
	ExpectedRoots   [][]byte
	ExpectedError   string
}

func (test *MsgSpecTest) Run(t *testing.T) {
	var lastErr error

	for i, byts := range test.EncodedMessages {
		m := &qbft.SignedMessage{}
		if err := m.Decode(byts); err != nil {
			lastErr = err
		}

		if len(test.ExpectedRoots) > 0 {
			r, err := m.GetRoot()
			if err != nil {
				lastErr = err
			}
			if !bytes.Equal(test.ExpectedRoots[i], r) {
				t.Fail()
			}
		}
	}

	for i, msg := range test.Messages {
		if err := msg.Validate(); err != nil {
			lastErr = err
		}

		if len(test.Messages) > 0 {
			r1, err := msg.Encode()
			if err != nil {
				lastErr = err
			}

			r2, err := test.Messages[i].Encode()
			if err != nil {
				lastErr = err
			}
			if !bytes.Equal(r2, r1) {
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

func (test *MsgSpecTest) TestName() string {
	return test.Name
}
