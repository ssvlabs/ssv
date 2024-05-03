package qbft

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	genesisspectests "github.com/ssvlabs/ssv-spec-pre-cc/qbft/spectest/tests"
)

func RunMsg(t *testing.T, test *genesisspectests.MsgSpecTest) { // using only spec struct so this test can be imported
	var lastErr error

	for i, msg := range test.Messages {
		if err := msg.Validate(); err != nil {
			lastErr = err
			continue
		}

		if msg.Message.RoundChangePrepared() && len(msg.Message.RoundChangeJustification) == 0 {
			lastErr = errors.New("round change justification invalid")
		}

		if len(test.EncodedMessages) > 0 {
			byts, err := msg.Encode()
			require.NoError(t, err)
			require.EqualValues(t, test.EncodedMessages[i], byts)
		}

		if len(test.ExpectedRoots) > 0 {
			r, err := msg.GetRoot()
			require.NoError(t, err)
			require.EqualValues(t, test.ExpectedRoots[i], r)
		}
	}

	// check error
	if len(test.ExpectedError) != 0 {
		require.EqualError(t, lastErr, test.ExpectedError)
	} else {
		require.NoError(t, lastErr)
	}
}
