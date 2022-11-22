package spectest

import (
	"encoding/hex"
	"testing"

	"github.com/bloxapp/ssv-spec/ssv"
	"github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/protocol/v2/ssv/runner"
	"github.com/bloxapp/ssv/protocol/v2/ssv/spectest/utils"
)

type MsgProcessingSpecTest struct {
	Name                    string
	Runner                  runner.Runner
	Duty                    *types.Duty
	Messages                []*types.SSVMessage
	PostDutyRunnerStateRoot string
	// OutputMessages compares pre/ post signed partial sigs to output. We exclude consensus msgs as it's tested in consensus
	OutputMessages []*ssv.SignedPartialSignatureMessage
	DontStartDuty  bool // if set to true will not start a duty for the runner
	ExpectedError  string
}

func (test *MsgProcessingSpecTest) TestName() string {
	return test.Name
}

func RunMsgProcessing(t *testing.T, test *MsgProcessingSpecTest) {
	v := utils.BaseValidator(testingutils.KeySetForShare(test.Runner.GetBaseRunner().Share))
	v.DutyRunners[test.Runner.GetBaseRunner().BeaconRoleType] = test.Runner
	// v.Network = test.Runner.GetNetwork() // TODO need to align

	var lastErr error
	if !test.DontStartDuty {
		lastErr = v.StartDuty(test.Duty)
	}
	for _, msg := range test.Messages {
		err := v.ProcessMessage(msg)
		if err != nil {
			lastErr = err
		}
	}

	if len(test.ExpectedError) != 0 {
		require.EqualError(t, lastErr, test.ExpectedError)
	} else {
		require.NoError(t, lastErr)
	}

	// test output message
	net := v.Network.(ssv.Network)
	broadcastedMsgs := net.(*testingutils.TestingNetwork).BroadcastedMsgs
	if len(broadcastedMsgs) > 0 {
		index := 0
		for _, msg := range broadcastedMsgs {
			if msg.MsgType != types.SSVPartialSignatureMsgType {
				continue
			}

			msg1 := &ssv.SignedPartialSignatureMessage{}
			require.NoError(t, msg1.Decode(msg.Data))
			msg2 := test.OutputMessages[index]
			require.Len(t, msg1.Message.Messages, len(msg2.Message.Messages))

			// messages are not guaranteed to be in order so we map them and then test all roots to be equal
			roots := make(map[string]string)
			for i, partialSigMsg2 := range msg2.Message.Messages {
				r2, err := partialSigMsg2.GetRoot()
				require.NoError(t, err)
				if _, found := roots[hex.EncodeToString(r2)]; !found {
					roots[hex.EncodeToString(r2)] = ""
				} else {
					roots[hex.EncodeToString(r2)] = hex.EncodeToString(r2)
				}

				partialSigMsg1 := msg1.Message.Messages[i]
				r1, err := partialSigMsg1.GetRoot()
				require.NoError(t, err)

				if _, found := roots[hex.EncodeToString(r1)]; !found {
					roots[hex.EncodeToString(r1)] = ""
				} else {
					roots[hex.EncodeToString(r1)] = hex.EncodeToString(r1)
				}
			}
			for k, v := range roots {
				require.EqualValues(t, k, v, "missing output msg")
			}

			index++
		}

		require.Len(t, test.OutputMessages, index)
	}

	// post root
	postRoot, err := test.Runner.GetRoot()
	require.NoError(t, err)
	require.EqualValues(t, test.PostDutyRunnerStateRoot, hex.EncodeToString(postRoot))
}
