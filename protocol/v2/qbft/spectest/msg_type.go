package qbft

import (
	"errors"
	"testing"

	spectests "github.com/ssvlabs/ssv-spec/qbft/spectest/tests"
	"github.com/stretchr/testify/require"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
)

func RunMsg(t *testing.T, test *spectests.MsgSpecTest) { // using only spec struct so this test can be imported
	var lastErr error

	for i, rc := range test.Messages {
		if err := rc.Validate(); err != nil {
			lastErr = err
			continue
		}
		msg, err := specqbft.DecodeMessage(rc.SSVMessage.Data)
		if err != nil {
			t.Fatal(err)
		}
		if msg.RoundChangePrepared() && len(msg.RoundChangeJustification) == 0 {
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
