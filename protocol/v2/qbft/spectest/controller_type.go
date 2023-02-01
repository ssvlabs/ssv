package qbft

import (
	"bytes"
	"encoding/hex"
	"reflect"
	"testing"

	qbfttesting "github.com/bloxapp/ssv/protocol/v2/qbft/testing"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectests "github.com/bloxapp/ssv-spec/qbft/spectest/tests"
	spectypes "github.com/bloxapp/ssv-spec/types"
	spectestingutils "github.com/bloxapp/ssv-spec/types/testingutils"
	"github.com/bloxapp/ssv/protocol/v2/qbft"
	"github.com/bloxapp/ssv/protocol/v2/qbft/controller"

	"github.com/stretchr/testify/require"
)

func RunControllerSpecTest(t *testing.T, test *spectests.ControllerSpecTest) {
	identifier := spectypes.NewMsgID(spectestingutils.TestingValidatorPubKey[:], spectypes.BNRoleAttester)
	config := qbfttesting.TestingConfig(spectestingutils.Testing4SharesSet(), identifier.GetRoleType())
	contr := qbfttesting.NewTestingQBFTController(
		identifier[:],
		spectestingutils.TestingShare(spectestingutils.Testing4SharesSet()),
		config,
		false,
	)

	var lastErr error
	for _, runData := range test.RunInstanceData {
		if err := runInstanceWithData(t, contr, config, identifier, runData); err != nil {
			lastErr = err
		}
	}

	if len(test.ExpectedError) != 0 {
		require.EqualError(t, lastErr, test.ExpectedError)
	} else {
		require.NoError(t, lastErr)
	}
}

func testTimer(
	t *testing.T,
	config *qbft.Config,
	runData *spectests.RunInstanceData,
) {
	if runData.ExpectedTimerState != nil {
		if timer, ok := config.GetTimer().(*spectestingutils.TestQBFTTimer); ok {
			require.Equal(t, runData.ExpectedTimerState.Timeouts, timer.State.Timeouts)
			require.Equal(t, runData.ExpectedTimerState.Round, timer.State.Round)
		}
	}
}

func testProcessMsg(
	t *testing.T,
	contr *controller.Controller,
	config *qbft.Config,
	runData *spectests.RunInstanceData,
) error {
	decidedCnt := 0
	var lastErr error
	for _, msg := range runData.InputMessages {
		decided, err := contr.ProcessMsg(msg)
		if err != nil {
			lastErr = err
		}
		if decided != nil {
			decidedCnt++

			data, _ := decided.Message.GetCommitData()
			require.EqualValues(t, runData.ExpectedDecidedState.DecidedVal, data.Data)
		}
	}
	require.EqualValues(t, runData.ExpectedDecidedState.DecidedCnt, decidedCnt)

	// verify sync decided by range calls
	if runData.ExpectedDecidedState.CalledSyncDecidedByRange {
		require.EqualValues(t, runData.ExpectedDecidedState.DecidedByRangeValues, config.GetNetwork().(*spectestingutils.TestingNetwork).DecidedByRange)
	} else {
		require.EqualValues(t, [2]specqbft.Height{0, 0}, config.GetNetwork().(*spectestingutils.TestingNetwork).DecidedByRange)
	}

	return lastErr
}

func testBroadcastedDecided(
	t *testing.T,
	config *qbft.Config,
	identifier spectypes.MessageID,
	runData *spectests.RunInstanceData,
) {
	if runData.ExpectedDecidedState.BroadcastedDecided != nil {
		// test broadcasted
		broadcastedMsgs := config.GetNetwork().(*spectestingutils.TestingNetwork).BroadcastedMsgs
		require.Greater(t, len(broadcastedMsgs), 0)
		found := false
		for _, msg := range broadcastedMsgs {
			if !bytes.Equal(identifier[:], msg.MsgID[:]) {
				continue
			}

			msg1 := &specqbft.SignedMessage{}
			require.NoError(t, msg1.Decode(msg.Data))
			r1, err := msg1.GetRoot()
			require.NoError(t, err)

			r2, err := runData.ExpectedDecidedState.BroadcastedDecided.GetRoot()
			require.NoError(t, err)

			if bytes.Equal(r1, r2) &&
				reflect.DeepEqual(runData.ExpectedDecidedState.BroadcastedDecided.Signers, msg1.Signers) &&
				reflect.DeepEqual(runData.ExpectedDecidedState.BroadcastedDecided.Signature, msg1.Signature) {
				require.False(t, found)
				found = true
			}
		}
		require.True(t, found)
	}
}

func runInstanceWithData(t *testing.T, contr *controller.Controller, config *qbft.Config, identifier spectypes.MessageID, runData *spectests.RunInstanceData) error {
	err := contr.StartNewInstance(runData.InputValue)
	var lastErr error
	if err != nil {
		lastErr = err
	}

	testTimer(t, config, runData)

	if err := testProcessMsg(t, contr, config, runData); err != nil {
		lastErr = err
	}

	testBroadcastedDecided(t, config, identifier, runData)

	// test root
	r, err := contr.GetRoot()
	require.NoError(t, err)
	require.EqualValues(t, runData.ControllerPostRoot, hex.EncodeToString(r))

	return lastErr
}
