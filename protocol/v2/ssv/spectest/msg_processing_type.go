package spectest

import (
	"encoding/hex"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	typescomparable "github.com/ssvlabs/ssv-spec/types/testingutils/comparable"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/validator"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
)

type MsgProcessingSpecTest struct {
	Name                    string
	Runner                  runner.Runner
	Duty                    spectypes.Duty
	Messages                []*spectypes.SignedSSVMessage
	PostDutyRunnerStateRoot string
	PostDutyRunnerState     spectypes.Root `json:"-"` // Field is ignored by encoding/json
	// OutputMessages compares pre/ post signed partial sigs to output. We exclude consensus msgs as it's tested in consensus
	OutputMessages         []*spectypes.PartialSignatureMessages
	BeaconBroadcastedRoots []string
	DontStartDuty          bool // if set to true will not start a duty for the runner
	ExpectedError          string
}

func (test *MsgProcessingSpecTest) TestName() string {
	return test.Name
}

func RunMsgProcessing(t *testing.T, test *MsgProcessingSpecTest) {
	logger := logging.TestLogger(t)
	test.overrideStateComparison(t)
	test.RunAsPartOfMultiTest(t, logger)
}

func (test *MsgProcessingSpecTest) runPreTesting(logger *zap.Logger) (*validator.Validator, error) {
	var share *spectypes.Share
	if len(test.Runner.GetBaseRunner().Share) == 0 {
		panic("No share in base runner for tests")
	}
	for _, validatorShare := range test.Runner.GetBaseRunner().Share {
		share = validatorShare
		break
	}
	v := protocoltesting.BaseValidator(logger, spectestingutils.KeySetForShare(share))
	v.DutyRunners[test.Runner.GetBaseRunner().RunnerRoleType] = test.Runner
	v.Network = test.Runner.GetNetwork()

	var lastErr error
	if !test.DontStartDuty {
		lastErr = v.StartDuty(logger, test.Duty)
	}
	for _, msg := range test.Messages {
		dmsg, err := queue.DecodeSignedSSVMessage(msg)
		if err != nil {
			lastErr = err
			continue
		}
		err = v.ProcessMessage(logger, dmsg)
		if err != nil {
			lastErr = err
		}
	}

	return v, lastErr
}

func (test *MsgProcessingSpecTest) RunAsPartOfMultiTest(t *testing.T, logger *zap.Logger) {
	v, lastErr := test.runPreTesting(logger)

	if len(test.ExpectedError) != 0 {
		require.EqualError(t, lastErr, test.ExpectedError)
	} else {
		require.NoError(t, lastErr)
	}

	// test output message
	test.compareOutputMsgs(t, v)

	// test beacon broadcasted msgs
	test.compareBroadcastedBeaconMsgs(t)

	// post root
	postRoot, err := test.Runner.GetRoot()
	require.NoError(t, err)

	if test.PostDutyRunnerStateRoot != hex.EncodeToString(postRoot[:]) {
		logger.Error("post runner state not equal", zap.String("state", cmp.Diff(test.Runner, test.PostDutyRunnerState, cmp.Exporter(func(p reflect.Type) bool { return true }))))
	}
}

func (test *MsgProcessingSpecTest) compareBroadcastedBeaconMsgs(t *testing.T) {
	broadcastedRoots := test.Runner.GetBeaconNode().(*spectestingutils.TestingBeaconNode).BroadcastedRoots
	require.Len(t, broadcastedRoots, len(test.BeaconBroadcastedRoots))
	for _, r1 := range test.BeaconBroadcastedRoots {
		found := false
		for _, r2 := range broadcastedRoots {
			if r1 == hex.EncodeToString(r2[:]) {
				found = true
				break
			}
		}
		require.Truef(t, found, "broadcasted beacon root not found")
	}
}

func (test *MsgProcessingSpecTest) compareOutputMsgs(t *testing.T, v *validator.Validator) {
	filterPartialSigs := func(messages []*spectypes.SSVMessage) []*spectypes.SSVMessage {
		ret := make([]*spectypes.SSVMessage, 0)
		for _, msg := range messages {
			if msg.MsgType != spectypes.SSVPartialSignatureMsgType {
				continue
			}
			ret = append(ret, msg)
		}
		return ret
	}
	broadcastedSignedMsgs := v.Network.(*spectestingutils.TestingNetwork).BroadcastedMsgs
	require.NoError(t, spectestingutils.VerifyListOfSignedSSVMessages(broadcastedSignedMsgs, v.Operator.Committee))
	broadcastedMsgs := spectestingutils.ConvertBroadcastedMessagesToSSVMessages(broadcastedSignedMsgs)
	broadcastedMsgs = filterPartialSigs(broadcastedMsgs)
	require.Len(t, broadcastedMsgs, len(test.OutputMessages))
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

		// test that slot is correct in broadcasted msg
		require.EqualValues(t, msg1.Slot, msg2.Slot, "incorrect broadcasted slot")

		index++
	}
}

func (test *MsgProcessingSpecTest) overrideStateComparison(t *testing.T) {
	testType := reflect.TypeOf(test).String()
	testType = strings.Replace(testType, "spectest.", "tests.", 1)
	overrideStateComparison(t, test, test.Name, testType)
}

func overrideStateComparison(t *testing.T, test *MsgProcessingSpecTest, name string, testType string) {
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
