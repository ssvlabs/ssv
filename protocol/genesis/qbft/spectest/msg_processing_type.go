package qbft

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectests "github.com/ssvlabs/ssv-spec-pre-cc/qbft/spectest/tests"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	genesisspectestingutils "github.com/ssvlabs/ssv-spec-pre-cc/types/testingutils"
	genesisspectypescomparable "github.com/ssvlabs/ssv-spec-pre-cc/types/testingutils/comparable"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/protocol/genesis/qbft"
	"github.com/ssvlabs/ssv/protocol/genesis/qbft/instance"
	"github.com/ssvlabs/ssv/protocol/genesis/qbft/roundtimer"
	qbfttesting "github.com/ssvlabs/ssv/protocol/genesis/qbft/testing"
	ssvtypes "github.com/ssvlabs/ssv/protocol/genesis/types"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
)

// RunMsgProcessing processes MsgProcessingSpecTest. It probably may be removed.
func RunMsgProcessing(t *testing.T, test *MsgProcessingSpecTest) {
	overrideStateComparisonForMsgProcessingSpecTest(t, test)

	// a little trick we do to instantiate all the internal instance params
	preByts, _ := test.Pre.Encode()
	msgId := genesisspecqbft.ControllerIdToMessageID(test.Pre.State.ID)
	logger := logging.TestLogger(t)
	pre := instance.NewInstance(
		qbfttesting.TestingConfig(logger, spectestingutils.KeySetForShare(test.Pre.State.Share), msgId.GetRoleType()),
		test.Pre.State.Share,
		test.Pre.State.ID,
		test.Pre.State.Height,
	)
	require.NoError(t, pre.Decode(preByts))

	preInstance := pre

	// a simple hack to change the proposer func
	if preInstance.State.Height == genesisspectests.ChangeProposerFuncInstanceHeight {
		preInstance.GetConfig().(*qbft.Config).ProposerF = func(state *ssvtypes.State, round genesisspecqbft.Round) genesisspectypes.OperatorID {
			return 2
		}
	}

	var lastErr error
	for _, msg := range test.InputMessages {
		_, _, _, err := preInstance.ProcessMsg(logger, msg)
		if err != nil {
			lastErr = err
		}
	}

	if len(test.ExpectedError) != 0 {
		require.EqualError(t, lastErr, test.ExpectedError, "expected %v, but got %v", test.ExpectedError, lastErr)
	} else {
		require.NoError(t, lastErr)
	}

	postRoot, err := preInstance.State.GetRoot()
	require.NoError(t, err)

	// broadcasting is asynchronic, so need to wait a bit before checking
	time.Sleep(time.Millisecond * 5)

	// test output message
	broadcastedMsgs := preInstance.GetConfig().GetNetwork().(*genesisspectestingutils.TestingNetwork).BroadcastedMsgs
	if len(test.OutputMessages) > 0 || len(broadcastedMsgs) > 0 {
		require.Len(t, broadcastedMsgs, len(test.OutputMessages))

		for i, msg := range test.OutputMessages {
			r1, _ := msg.GetRoot()

			ssvMsg := &genesisspectypes.SSVMessage{}
			require.NoError(t, ssvMsg.Decode(broadcastedMsgs[i].Data))

			msg2 := &genesisspecqbft.SignedMessage{}
			require.NoError(t, msg2.Decode(ssvMsg.Data))
			r2, _ := msg2.GetRoot()

			require.EqualValues(t, r1, r2, fmt.Sprintf("output msg %d roots not equal", i))
		}
	}

	require.EqualValues(t, test.PostRoot, hex.EncodeToString(postRoot[:]), "post root not valid")
}

func overrideStateComparisonForMsgProcessingSpecTest(t *testing.T, test *MsgProcessingSpecTest) {
	specDir, err := protocoltesting.GetSpecDir("", filepath.Join("qbft", "spectest"))
	require.NoError(t, err)
	test.PostState, err = genesisspectypescomparable.UnmarshalStateComparison(specDir, test.TestName(),
		reflect.TypeOf(test).String(),
		&genesisspecqbft.State{})
	require.NoError(t, err)

	r, err := test.PostState.GetRoot()
	require.NoError(t, err)

	// backwards compatability test, hard coded post root must be equal to the one loaded from file
	if len(test.PostRoot) > 0 {
		require.EqualValues(t, test.PostRoot, hex.EncodeToString(r[:]))
	}

	test.PostRoot = hex.EncodeToString(r[:])
}

type MsgProcessingSpecTest struct {
	Name               string
	Pre                *instance.Instance
	PostRoot           string
	PostState          spectypes.Root `json:"-"` // Field is ignored by encoding/json
	InputMessages      []*genesisspecqbft.SignedMessage
	OutputMessages     []*genesisspecqbft.SignedMessage
	ExpectedError      string
	ExpectedTimerState *genesisspectestingutils.TimerState
}

func (test *MsgProcessingSpecTest) Run(t *testing.T) {
	// temporary to override state comparisons from file not inputted one
	test.overrideStateComparison(t)

	lastErr := test.runPreTesting()

	if len(test.ExpectedError) != 0 {
		require.EqualError(t, lastErr, test.ExpectedError)
	} else {
		require.NoError(t, lastErr)
	}

	if test.ExpectedTimerState != nil {
		// checks round timer state
		timer, ok := test.Pre.GetConfig().GetTimer().(*roundtimer.TestQBFTTimer)
		if ok && timer != nil {
			require.Equal(t, test.ExpectedTimerState.Timeouts, timer.State.Timeouts, "timer should have expected timeouts count")
			require.Equal(t, test.ExpectedTimerState.Round, timer.State.Round, "timer should have expected round")
		}
	}

	postRoot, err := test.Pre.State.GetRoot()
	require.NoError(t, err)

	// test output message
	broadcastedSignedMsgs := test.Pre.GetConfig().GetNetwork().(*genesisspectestingutils.TestingNetwork).BroadcastedMsgs
	require.NoError(t, genesisspectestingutils.VerifyListOfSignedSSVMessages(broadcastedSignedMsgs, test.Pre.State.Share.Committee))
	broadcastedMsgs := genesisspectestingutils.ConvertBroadcastedMessagesToSSVMessages(broadcastedSignedMsgs)
	if len(test.OutputMessages) > 0 || len(broadcastedMsgs) > 0 {
		require.Len(t, broadcastedMsgs, len(test.OutputMessages))

		for i, msg := range test.OutputMessages {
			r1, _ := msg.GetRoot()

			msg2 := &genesisspecqbft.SignedMessage{}
			require.NoError(t, msg2.Decode(broadcastedMsgs[i].Data))
			r2, _ := msg2.GetRoot()

			require.EqualValues(t, r1, r2, fmt.Sprintf("output msg %d roots not equal", i))
		}
	}

	// test root
	if test.PostRoot != hex.EncodeToString(postRoot[:]) {
		diff := genesisspectypescomparable.PrintDiff(test.Pre.State, test.PostState)
		require.Fail(t, fmt.Sprintf("expected root: %s\nactual root: %s\n\n", test.PostRoot, hex.EncodeToString(postRoot[:])), "post state not equal", diff)
	}
}

func (test *MsgProcessingSpecTest) runPreTesting() error {
	// a simple hack to change the proposer func
	if test.Pre.State.Height == genesisspectests.ChangeProposerFuncInstanceHeight {
		test.Pre.GetConfig().(*qbft.Config).ProposerF = func(state *ssvtypes.State, round genesisspecqbft.Round) spectypes.OperatorID {
			return 2
		}
	}

	var lastErr error
	for _, msg := range test.InputMessages {
		_, _, _, err := test.Pre.ProcessMsg(zap.NewNop(), msg)
		if err != nil {
			lastErr = err
		}
	}
	return lastErr
}

func (test *MsgProcessingSpecTest) TestName() string {
	return "qbft message processing " + test.Name
}

func (test *MsgProcessingSpecTest) overrideStateComparison(t *testing.T) {
	basedir, err := os.Getwd()
	require.NoError(t, err)
	test.PostState, err = genesisspectypescomparable.UnmarshalStateComparison(basedir, test.TestName(),
		reflect.TypeOf(test).String(),
		&specqbft.State{})
	require.NoError(t, err)

	r, err := test.PostState.GetRoot()
	require.NoError(t, err)

	// backwards compatability test, hard coded post root must be equal to the one loaded from file
	if len(test.PostRoot) > 0 {
		require.EqualValues(t, test.PostRoot, hex.EncodeToString(r[:]))
	}

	test.PostRoot = hex.EncodeToString(r[:])
}

func (test *MsgProcessingSpecTest) GetPostState() (interface{}, error) {
	err := test.runPreTesting()
	if err != nil && len(test.ExpectedError) == 0 { // only non expected errors should return error
		return nil, err
	}
	return test.Pre.State, nil
}
