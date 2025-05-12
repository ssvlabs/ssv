package qbft

import (
	"context"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectests "github.com/ssvlabs/ssv-spec/qbft/spectest/tests"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	typescomparable "github.com/ssvlabs/ssv-spec/types/testingutils/comparable"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/protocol/v2/qbft"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
	qbfttesting "github.com/ssvlabs/ssv/protocol/v2/qbft/testing"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
)

// RunMsgProcessing processes MsgProcessingSpecTest. It probably may be removed.
func RunMsgProcessing(t *testing.T, test *spectests.MsgProcessingSpecTest) {
	overrideStateComparisonForMsgProcessingSpecTest(t, test)

	// a little trick we do to instantiate all the internal instance params
	preByts, _ := test.Pre.Encode()
	logger := logging.TestLogger(t)
	ks := spectestingutils.KeySetForCommitteeMember(test.Pre.State.CommitteeMember)
	signer := spectestingutils.NewOperatorSigner(ks, 1)
	pre := instance.NewInstance(
		qbfttesting.TestingConfig(logger, ks),
		test.Pre.State.CommitteeMember,
		test.Pre.State.ID,
		test.Pre.State.Height,
		signer,
	)
	require.NoError(t, pre.Decode(preByts))

	preInstance := pre

	// a simple hack to change the proposer func
	if preInstance.State.Height == spectests.ChangeProposerFuncInstanceHeight {
		preInstance.GetConfig().(*qbft.Config).ProposerF = func(state *specqbft.State, round specqbft.Round) spectypes.OperatorID {
			return 2
		}
	}

	var lastErr error
	for _, msg := range test.InputMessages {
		_, _, _, err := preInstance.ProcessMsg(context.TODO(), logger, spectestingutils.ToProcessingMessage(msg))
		if err != nil {
			lastErr = err
		}
	}
	if test.ExpectedError != "" {
		require.EqualError(t, lastErr, test.ExpectedError)
	} else {
		require.NoError(t, lastErr)
	}

	postRoot, err := preInstance.State.GetRoot()
	require.NoError(t, err)

	// broadcasting is asynchronic, so need to wait a bit before checking
	time.Sleep(time.Millisecond * 5)

	// test output message
	broadcastedMsgs := preInstance.GetConfig().GetNetwork().(*spectestingutils.TestingNetwork).BroadcastedMsgs
	if len(test.OutputMessages) > 0 || len(broadcastedMsgs) > 0 {
		require.Len(t, broadcastedMsgs, len(test.OutputMessages))

		for i, msg := range test.OutputMessages {
			r1, _ := msg.GetRoot()
			msg2 := broadcastedMsgs[i]
			r2, _ := msg2.GetRoot()
			require.EqualValues(t, r1, r2, fmt.Sprintf("output msg %d roots not equal", i))
		}
	}

	require.EqualValues(t, test.PostRoot, hex.EncodeToString(postRoot[:]), "post root not valid")
}

func overrideStateComparisonForMsgProcessingSpecTest(t *testing.T, test *spectests.MsgProcessingSpecTest) {
	specDir, err := protocoltesting.GetSpecDir("", filepath.Join("qbft", "spectest"))
	require.NoError(t, err)
	test.PostState, err = typescomparable.UnmarshalStateComparison(specDir, test.TestName(),
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
