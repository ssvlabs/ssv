package qbft

import (
	"context"
	"encoding/hex"
	"fmt"
	"testing"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"
)

type SpecTest struct {
	Name               string
	Pre                *instance.Instance
	PostRoot           string
	OutputMessages     []*spectypes.SignedSSVMessage
	ExpectedTimerState *testingutils.TimerState
	ExpectedError      string
}

func RunTimeout(t *testing.T, test *SpecTest) {
	logger := logging.TestLogger(t)
	err := test.Pre.UponRoundTimeout(context.TODO(), logger)
	if test.ExpectedError != "" {
		require.EqualError(t, err, test.ExpectedError)
	} else {
		require.NoError(t, err)
	}

	// test calling timeout
	timer, ok := test.Pre.GetConfig().GetTimer().(*roundtimer.TestQBFTTimer)
	require.True(t, ok)
	require.Equal(t, test.ExpectedTimerState.Timeouts, timer.State.Timeouts)
	require.Equal(t, test.ExpectedTimerState.Round, timer.State.Round)

	// test output message
	broadcastedMsgs := test.Pre.GetConfig().GetNetwork().(*testingutils.TestingNetwork).BroadcastedMsgs
	if len(test.OutputMessages) > 0 || len(broadcastedMsgs) > 0 {
		require.Len(t, broadcastedMsgs, len(test.OutputMessages))

		for i, msg := range test.OutputMessages {
			r1, _ := msg.GetRoot()

			msg2 := broadcastedMsgs[i]
			r2, _ := msg2.GetRoot()

			require.EqualValuesf(t, r1, r2, fmt.Sprintf("output msg %d roots not equal", i))
		}
	}

	postRoot, err := test.Pre.State.GetRoot()
	require.NoError(t, err)
	require.EqualValuesf(t, test.PostRoot, hex.EncodeToString(postRoot[:]), "post root not valid")
}
