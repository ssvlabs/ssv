package qbft

import (
	"encoding/hex"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/protocol/v2/genesisqbft"
	"github.com/bloxapp/ssv/protocol/v2/genesisqbft/instance"
	qbfttesting "github.com/bloxapp/ssv/protocol/v2/genesisqbft/testing"
	protocoltesting "github.com/bloxapp/ssv/protocol/v2/testing"
	"github.com/stretchr/testify/require"

	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectests "github.com/ssvlabs/ssv-spec-pre-cc/qbft/spectest/tests"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	genesisspectestingutils "github.com/ssvlabs/ssv-spec-pre-cc/types/testingutils"
	typescomparable "github.com/ssvlabs/ssv-spec-pre-cc/types/testingutils/comparable"
)

// RunMsgProcessing processes MsgProcessingSpecTest. It probably may be removed.
func RunMsgProcessing(t *testing.T, test *genesisspectests.MsgProcessingSpecTest) {
	overrideStateComparisonForMsgProcessingSpecTest(t, test)

	// a little trick we do to instantiate all the internal instance params
	preByts, _ := test.Pre.Encode()
	msgId := genesisspecqbft.ControllerIdToMessageID(test.Pre.State.ID)
	logger := logging.TestLogger(t)
	pre := instance.NewInstance(
		qbfttesting.TestingConfig(logger, genesisspectestingutils.KeySetForShare(test.Pre.State.Share), msgId.GetRoleType()),
		test.Pre.State.Share,
		test.Pre.State.ID,
		test.Pre.State.Height,
	)
	require.NoError(t, pre.Decode(preByts))

	preInstance := pre

	// a simple hack to change the proposer func
	if preInstance.State.Height == genesisspectests.ChangeProposerFuncInstanceHeight {
		preInstance.GetConfig().(*genesisqbft.Config).ProposerF = func(state *genesisspecqbft.State, round genesisspecqbft.Round) genesisspectypes.OperatorID {
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

			msg2 := &genesisspecqbft.SignedMessage{}
			require.NoError(t, msg2.Decode(broadcastedMsgs[i].Data))
			r2, _ := msg2.GetRoot()

			require.EqualValues(t, r1, r2, fmt.Sprintf("output msg %d roots not equal", i))
		}
	}

	require.EqualValues(t, test.PostRoot, hex.EncodeToString(postRoot[:]), "post root not valid")
}

func overrideStateComparisonForMsgProcessingSpecTest(t *testing.T, test *genesisspectests.MsgProcessingSpecTest) {
	specDir, err := protocoltesting.GetSpecDir("", filepath.Join("qbft", "spectest"))
	require.NoError(t, err)
	test.PostState, err = typescomparable.UnmarshalStateComparison(specDir, test.TestName(),
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
