package spectest

import (
	"encoding/hex"
	"testing"

	"github.com/bloxapp/ssv-spec/ssv"
	"github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/protocol/v2/ssv/runner"
)

type StartNewRunnerDutySpecTest struct {
	Name                    string
	Runner                  runner.Runner
	Duty                    *types.Duty
	PostDutyRunnerStateRoot string
	OutputMessages          []*ssv.SignedPartialSignatureMessage
	ExpectedError           string
}

func (test *StartNewRunnerDutySpecTest) TestName() string {
	return test.Name
}

func (test *StartNewRunnerDutySpecTest) Run(t *testing.T) {
	err := test.Runner.StartNewDuty(test.Duty)
	if len(test.ExpectedError) > 0 {
		require.EqualError(t, err, test.ExpectedError)
	} else {
		require.NoError(t, err)
	}

	// test output message
	broadcastedMsgs := test.Runner.GetNetwork().(*testingutils.TestingNetwork).BroadcastedMsgs
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

type MultiStartNewRunnerDutySpecTest struct {
	Name  string
	Tests []*StartNewRunnerDutySpecTest
}

func (tests *MultiStartNewRunnerDutySpecTest) TestName() string {
	return tests.Name
}

func (tests *MultiStartNewRunnerDutySpecTest) Run(t *testing.T) {
	for _, test := range tests.Tests {
		test := test
		t.Run(test.TestName(), func(t *testing.T) {
			test.Run(t)
		})
	}
}
