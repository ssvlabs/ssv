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

	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectests "github.com/ssvlabs/ssv-spec-pre-cc/qbft/spectest/tests"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	genesisspectestingutils "github.com/ssvlabs/ssv-spec-pre-cc/types/testingutils"
	typescomparable "github.com/ssvlabs/ssv-spec-pre-cc/types/testingutils/comparable"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/protocol/genesis/qbft"
	"github.com/ssvlabs/ssv/protocol/genesis/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/genesis/qbft/roundtimer"
	qbfttesting "github.com/ssvlabs/ssv/protocol/genesis/qbft/testing"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
)

func RunControllerSpecTest(t *testing.T, test *genesisspectests.ControllerSpecTest) {
	//temporary to override state comparisons from file not inputted one
	overrideStateComparisonForControllerSpecTest(t, test)

	logger := logging.TestLogger(t)
	contr := generateController(logger)

	if test.StartHeight != nil {
		contr.Height = *test.StartHeight
	}

	var lastErr error
	for i, runData := range test.RunInstanceData {
		height := genesisspecqbft.Height(i)
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
	config := qbfttesting.TestingConfig(logger, spectestingutils.Testing4SharesSet(), genesisspectypes.BNRoleAttester)
	return qbfttesting.NewTestingQBFTController(
		identifier[:],
		spectestingutils.TestingShare(spectestingutils.Testing4SharesSet(), 1),
		config,
		false,
	)
}

func testTimer(
	t *testing.T,
	config *qbft.Config,
	runData *genesisspectests.RunInstanceData,
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
	runData *genesisspectests.RunInstanceData,
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
	runData *genesisspectests.RunInstanceData,
	operators []*spectypes.ShareMember,
) {
	if runData.ExpectedDecidedState.BroadcastedDecided != nil {
		// test broadcasted
		broadcastedSignedMsgs := config.GetNetwork().(*genesisspectestingutils.TestingNetwork).BroadcastedMsgs
		require.Greater(t, len(broadcastedSignedMsgs), 0)
		require.NoError(t, genesisspectestingutils.VerifyListOfSignedSSVMessages(broadcastedSignedMsgs, operators))
		broadcastedMsgs := genesisspectestingutils.ConvertBroadcastedMessagesToSSVMessages(broadcastedSignedMsgs)
		found := false
		for _, msg := range broadcastedMsgs {

			// a hack for testing non standard messageID identifiers since we copy them into a MessageID this fixes it
			msgID := genesisspectypes.MessageID{}
			copy(msgID[:], identifier)

			if !bytes.Equal(msgID[:], msg.MsgID[:]) {
				continue
			}

			msg1 := &genesisspecqbft.SignedMessage{}
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

func runInstanceWithData(t *testing.T, logger *zap.Logger, height genesisspecqbft.Height, contr *controller.Controller, runData *genesisspectests.RunInstanceData) error {
	err := contr.StartNewInstance(logger, height, runData.InputValue)
	var lastErr error
	if err != nil {
		lastErr = err
	}

	testTimer(t, contr.GetConfig().(*qbft.Config), runData)

	if err := testProcessMsg(t, logger, contr, contr.GetConfig().(*qbft.Config), runData); err != nil {
		lastErr = err
	}

	testBroadcastedDecided(t, contr.GetConfig().(*qbft.Config), contr.Identifier, runData, contr.Share.Committee)

	// test root
	r, err := contr.GetRoot()
	require.NoError(t, err)
	require.EqualValues(t, runData.ControllerPostRoot, hex.EncodeToString(r[:]))

	return lastErr
}

func overrideStateComparisonForControllerSpecTest(t *testing.T, test *genesisspectests.ControllerSpecTest) {
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

func VerifyListOfSignedSSVMessages(msgs []*genesisspectypes.SignedSSVMessage, operators []*genesisspectypes.Operator) error {
	verifier := genesisspectestingutils.NewTestingVerifier()

	for _, msg := range msgs {
		err := verifier.Verify(msg, operators)
		if err != nil {
			return err
		}
	}
	return nil
}
