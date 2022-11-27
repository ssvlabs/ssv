package qbft

import (
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv-spec/qbft"
	spectests "github.com/bloxapp/ssv-spec/qbft/spectest/tests"
	"github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv-spec/types/testingutils"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v2/qbft/instance"
	"github.com/bloxapp/ssv/protocol/v2/ssv/spectest/utils"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func RunMsgProcessing(t *testing.T, test *spectests.MsgProcessingSpecTest) {

	// a little trick we do to instantiate all the internal instance params
	preByts, _ := test.Pre.Encode()
	msgId := message.ToMessageID(test.Pre.State.ID)
	pre := instance.NewInstance(
		utils.TestingConfig(testingutils.KeySetForShare(test.Pre.State.Share), msgId.GetRoleType()),
		test.Pre.State.Share,
		test.Pre.State.ID,
		test.Pre.State.Height,
	)
	require.NoError(t, pre.Decode(preByts))

	preInstance := pre

	// a simple hack to change the proposer func
	if preInstance.State.Height == spectests.ChangeProposerFuncInstanceHeight {
		preInstance.GetConfig().(*ssvtypes.Config).ProposerF = func(state *qbft.State, round qbft.Round) types.OperatorID {
			return 2
		}
	}

	var lastErr error
	for _, msg := range test.InputMessages {
		_, _, _, err := preInstance.ProcessMsg(msg)
		if err != nil {
			lastErr = err
		}
	}

	if len(test.ExpectedError) != 0 {
		require.EqualError(t, lastErr, test.ExpectedError)
	} else {
		require.NoError(t, lastErr)
	}

	postRoot, err := preInstance.State.GetRoot()
	require.NoError(t, err)

	// broadcasting is asynchronic, so need to wait a bit before checking
	time.Sleep(time.Millisecond * 50)

	// test output message
	broadcastedMsgs := preInstance.GetConfig().GetNetwork().(*testingutils.TestingNetwork).BroadcastedMsgs
	if len(test.OutputMessages) > 0 || len(broadcastedMsgs) > 0 {
		require.Len(t, broadcastedMsgs, len(test.OutputMessages))

		for i, msg := range test.OutputMessages {
			r1, _ := msg.GetRoot()

			msg2 := &qbft.SignedMessage{}
			require.NoError(t, msg2.Decode(broadcastedMsgs[i].Data))
			r2, _ := msg2.GetRoot()

			require.EqualValues(t, r1, r2, fmt.Sprintf("output msg %d roots not equal", i))
		}
	}

	require.EqualValues(t, test.PostRoot, hex.EncodeToString(postRoot), "post root not valid")
}
