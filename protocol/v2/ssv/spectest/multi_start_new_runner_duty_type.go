package spectest

import (
	"context"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	typescomparable "github.com/ssvlabs/ssv-spec/types/testingutils/comparable"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
)

type StartNewRunnerDutySpecTest struct {
	Name                    string
	Runner                  runner.Runner
	Duty                    spectypes.Duty
	Threshold               uint64
	PostDutyRunnerStateRoot string
	PostDutyRunnerState     spectypes.Root `json:"-"` // Field is ignored by encoding/json
	OutputMessages          []*spectypes.PartialSignatureMessages
	ExpectedError           string
}

func (test *StartNewRunnerDutySpecTest) TestName() string {
	return test.Name
}

// overrideStateComparison overrides the state comparison to compare the runner state
func (test *StartNewRunnerDutySpecTest) overrideStateComparison(t *testing.T) {
	testType := reflect.TypeOf(test).String()
	testType = strings.Replace(testType, "spectest.", "newduty.", 1)
	overrideStateComparisonForStartNewRunnerDutySpecTest(t, test, test.Name, testType)
}

func (test *StartNewRunnerDutySpecTest) RunAsPartOfMultiTest(t *testing.T, logger *zap.Logger) {
	err := test.runPreTesting(logger)
	if len(test.ExpectedError) > 0 {
		require.EqualError(t, err, test.ExpectedError)
	} else {
		require.NoError(t, err)
	}

	// test output message
	broadcastedSignedMsgs := test.Runner.GetNetwork().(*spectestingutils.TestingNetwork).BroadcastedMsgs
	broadcastedMsgs := spectestingutils.ConvertBroadcastedMessagesToSSVMessages(broadcastedSignedMsgs)
	if len(broadcastedMsgs) > 0 {
		index := 0
		for _, msg := range broadcastedMsgs {
			if msg.MsgType != spectypes.SSVPartialSignatureMsgType {
				continue
			}

			msg1 := &spectypes.PartialSignatureMessages{}
			require.NoError(t, msg1.Decode(msg.Data))
			msg2 := test.OutputMessages[index]
			require.Len(t, msg1.Messages, len(msg2.Messages))

			// messages are not guaranteed to be in order so we map them and then test all roots to be equal
			roots := make(map[string]string)
			for i, partialSigMsg2 := range msg2.Messages {
				r2, err := partialSigMsg2.GetRoot()
				require.NoError(t, err)
				if _, found := roots[hex.EncodeToString(r2[:])]; !found {
					roots[hex.EncodeToString(r2[:])] = ""
				} else {
					roots[hex.EncodeToString(r2[:])] = hex.EncodeToString(r2[:])
				}

				partialSigMsg1 := msg1.Messages[i]
				r1, err := partialSigMsg1.GetRoot()
				require.NoError(t, err)

				if _, found := roots[hex.EncodeToString(r1[:])]; !found {
					roots[hex.EncodeToString(r1[:])] = ""
				} else {
					roots[hex.EncodeToString(r1[:])] = hex.EncodeToString(r1[:])
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

	if test.PostDutyRunnerStateRoot != hex.EncodeToString(postRoot[:]) {
		diff := dumpState(t, test.Name, test.Runner, test.PostDutyRunnerState)
		require.EqualValues(t, test.PostDutyRunnerStateRoot, hex.EncodeToString(postRoot[:]), fmt.Sprintf("post runner state not equal\n%s\n", diff))
	}
}

func (test *StartNewRunnerDutySpecTest) Run(t *testing.T, logger *zap.Logger) {
	test.overrideStateComparison(t)
	test.RunAsPartOfMultiTest(t, logger)
}

type MultiStartNewRunnerDutySpecTest struct {
	Name  string
	Tests []*StartNewRunnerDutySpecTest
}

func (tests *MultiStartNewRunnerDutySpecTest) TestName() string {
	return tests.Name
}

func (tests *MultiStartNewRunnerDutySpecTest) Run(t *testing.T, logger *zap.Logger) {
	tests.overrideStateComparison(t)

	for _, test := range tests.Tests {
		t.Run(test.TestName(), func(t *testing.T) {
			test.RunAsPartOfMultiTest(t, logger)
		})
	}
}

// overrideStateComparison overrides the post state comparison for all tests in the multi test
func (tests *MultiStartNewRunnerDutySpecTest) overrideStateComparison(t *testing.T) {
	testsName := strings.ReplaceAll(tests.TestName(), " ", "_")
	for _, test := range tests.Tests {
		path := filepath.Join(testsName, test.TestName())
		testType := reflect.TypeOf(tests).String()
		testType = strings.Replace(testType, "spectest.", "newduty.", 1)
		overrideStateComparisonForStartNewRunnerDutySpecTest(t, test, path, testType)
	}
}

func overrideStateComparisonForStartNewRunnerDutySpecTest(t *testing.T, test *StartNewRunnerDutySpecTest, name string, testType string) {
	var r runner.Runner
	switch test.Runner.(type) {
	case *runner.CommitteeRunner:
		r = &runner.CommitteeRunner{}
	case *runner.AggregatorRunner:
		r = &runner.AggregatorRunner{}
	case *runner.ProposerRunner:
		r = &runner.ProposerRunner{}
	case *runner.SyncCommitteeAggregatorRunner:
		r = &runner.SyncCommitteeAggregatorRunner{}
	case *runner.ValidatorRegistrationRunner:
		r = &runner.ValidatorRegistrationRunner{}
	case *runner.VoluntaryExitRunner:
		r = &runner.VoluntaryExitRunner{}
	default:
		t.Fatalf("unknown runner type")
	}
	specDir, err := protocoltesting.GetSpecDir("", filepath.Join("ssv", "spectest"))
	require.NoError(t, err)
	r, err = typescomparable.UnmarshalStateComparison(specDir, name, testType, r)
	require.NoError(t, err)

	// override
	test.PostDutyRunnerState = r

	root, err := r.GetRoot()
	require.NoError(t, err)

	test.PostDutyRunnerStateRoot = hex.EncodeToString(root[:])
}

func (test *StartNewRunnerDutySpecTest) runPreTesting(logger *zap.Logger) error {
	err := test.Runner.StartNewDuty(context.TODO(), logger, test.Duty, test.Threshold)
	return err
}
