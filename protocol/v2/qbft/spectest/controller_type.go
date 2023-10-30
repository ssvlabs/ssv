package qbft

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectests "github.com/bloxapp/ssv-spec/qbft/spectest/tests"
	spectypes "github.com/bloxapp/ssv-spec/types"
	spectestingutils "github.com/bloxapp/ssv-spec/types/testingutils"
	typescomparable "github.com/bloxapp/ssv-spec/types/testingutils/comparable"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/protocol/v2/qbft"
	"github.com/bloxapp/ssv/protocol/v2/qbft/controller"
	"github.com/bloxapp/ssv/protocol/v2/qbft/roundtimer"
	qbfttesting "github.com/bloxapp/ssv/protocol/v2/qbft/testing"
	protocoltesting "github.com/bloxapp/ssv/protocol/v2/testing"
)

func RunControllerSpecTest(t *testing.T, test *spectests.ControllerSpecTest) {
	//temporary to override state comparisons from file not inputted one
	overrideStateComparisonForControllerSpecTest(t, test)

	logger := logging.TestLogger(t)
	contr := generateController(logger)

	var lastErr error
	for i, runData := range test.RunInstanceData {
		height := specqbft.Height(i)
		if runData.Height != nil {
			height = *runData.Height
		}
		if err := runInstanceWithData(t, logger, height, contr, runData); err != nil {
			lastErr = err
		}
	}

	if len(test.ExpectedError) != 0 {
		require.EqualError(t, lastErr, test.ExpectedError)
	} else {
		require.NoError(t, lastErr)
	}
}

func generateController(logger *zap.Logger) *controller.Controller {
	identifier := []byte{1, 2, 3, 4}
	config := qbfttesting.TestingConfig(logger, spectestingutils.Testing4SharesSet(), spectypes.BNRoleAttester)
	return qbfttesting.NewTestingQBFTController(
		identifier[:],
		spectestingutils.TestingShare(spectestingutils.Testing4SharesSet()),
		config,
		false,
	)
}

func testTimer(
	t *testing.T,
	config *qbft.Config,
	runData *spectests.RunInstanceData,
) {
	if runData.ExpectedTimerState != nil {
		if timer, ok := config.GetTimer().(*roundtimer.TestQBFTTimer); ok {
			require.Equal(t, runData.ExpectedTimerState.Timeouts, timer.State.Timeouts)
			require.Equal(t, runData.ExpectedTimerState.Round, timer.State.Round)
		}
	}
}

func testProcessMsg(
	t *testing.T,
	logger *zap.Logger,
	contr *controller.Controller,
	config *qbft.Config,
	runData *spectests.RunInstanceData,
) error {
	decidedCnt := uint(0)
	var lastErr error
	for _, msg := range runData.InputMessages {
		decided, err := contr.ProcessMsg(logger, msg)
		if err != nil {
			lastErr = err
		}
		if decided != nil {
			decidedCnt++

			require.EqualValues(t, runData.ExpectedDecidedState.DecidedVal, decided.FullData)
		}
	}
	require.EqualValues(t, runData.ExpectedDecidedState.DecidedCnt, decidedCnt, lastErr)

	return lastErr
}

func testBroadcastedDecided(
	t *testing.T,
	config *qbft.Config,
	identifier []byte,
	runData *spectests.RunInstanceData,
) {
	if runData.ExpectedDecidedState.BroadcastedDecided != nil {
		// test broadcasted
		broadcastedMsgs := config.GetNetwork().(*spectestingutils.TestingNetwork).BroadcastedMsgs
		require.Greater(t, len(broadcastedMsgs), 0)
		found := false
		for _, msg := range broadcastedMsgs {

			// a hack for testing non standard messageID identifiers since we copy them into a MessageID this fixes it
			msgID := spectypes.MessageID{}
			copy(msgID[:], identifier)

			if !bytes.Equal(msgID[:], msg.MsgID[:]) {
				continue
			}

			msg1 := &specqbft.SignedMessage{}
			require.NoError(t, msg1.Decode(msg.Data))
			r1, err := msg1.GetRoot()
			require.NoError(t, err)

			r2, err := runData.ExpectedDecidedState.BroadcastedDecided.GetRoot()
			require.NoError(t, err)

			if r1 == r2 &&
				reflect.DeepEqual(runData.ExpectedDecidedState.BroadcastedDecided.Signers, msg1.Signers) &&
				reflect.DeepEqual(runData.ExpectedDecidedState.BroadcastedDecided.Signature, msg1.Signature) {
				require.False(t, found)
				found = true
			}
		}
		require.True(t, found)
	}
}

func runInstanceWithData(t *testing.T, logger *zap.Logger, height specqbft.Height, contr *controller.Controller, runData *spectests.RunInstanceData) error {
	err := contr.StartNewInstance(logger, height, runData.InputValue)
	var lastErr error
	if err != nil {
		lastErr = err
	}

	testTimer(t, contr.GetConfig().(*qbft.Config), runData)

	if err := testProcessMsg(t, logger, contr, contr.GetConfig().(*qbft.Config), runData); err != nil {
		lastErr = err
	}

	testBroadcastedDecided(t, contr.GetConfig().(*qbft.Config), contr.Identifier, runData)

	// test root
	r, err := contr.GetRoot()
	require.NoError(t, err)
	require.EqualValues(t, runData.ControllerPostRoot, hex.EncodeToString(r[:]))

	return lastErr
}

func overrideStateComparisonForControllerSpecTest(t *testing.T, test *spectests.ControllerSpecTest) {
	specDir, err := protocoltesting.GetSpecDir("", filepath.Join("qbft", "spectest"))
	require.NoError(t, err)
	specDir = filepath.Join(specDir, "generate")
	dir := typescomparable.GetSCDir(specDir, reflect.TypeOf(test).String())
	path := filepath.Join(dir, fmt.Sprintf("%s.json", test.TestName()))
	byteValue, err := os.ReadFile(filepath.Clean(path))
	require.NoError(t, err)
	sc := make([]*controller.Controller, len(test.RunInstanceData))
	require.NoError(t, json.Unmarshal(byteValue, &sc))

	for i, runData := range test.RunInstanceData {
		runData.ControllerPostState = sc[i]

		r, err := sc[i].GetRoot()
		require.NoError(t, err)

		runData.ControllerPostRoot = hex.EncodeToString(r[:])
	}
}
