package spectest

import (
	"encoding/hex"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	specssv "github.com/bloxapp/ssv-spec/ssv"
	spectypes "github.com/bloxapp/ssv-spec/types"
	spectestingutils "github.com/bloxapp/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	typescomparable "github.com/bloxapp/ssv-spec/types/testingutils/comparable"
	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
	"github.com/bloxapp/ssv/protocol/v2/ssv/runner"
	ssvtesting "github.com/bloxapp/ssv/protocol/v2/ssv/testing"
	"github.com/bloxapp/ssv/protocol/v2/ssv/validator"
	protocoltesting "github.com/bloxapp/ssv/protocol/v2/testing"
)

type MsgProcessingSpecTest struct {
	Name                    string
	Runner                  runner.Runner
	Duty                    *spectypes.Duty
	Messages                []*spectypes.SSVMessage
	PostDutyRunnerStateRoot string
	PostDutyRunnerState     spectypes.Root `json:"-"` // Field is ignored by encoding/json
	// OutputMessages compares pre/ post signed partial sigs to output. We exclude consensus msgs as it's tested in consensus
	OutputMessages         []*spectypes.SignedPartialSignatureMessage
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

func (test *MsgProcessingSpecTest) RunAsPartOfMultiTest(t *testing.T, logger *zap.Logger) {
	v := ssvtesting.BaseValidator(logger, spectestingutils.KeySetForShare(test.Runner.GetBaseRunner().Share))
	v.DutyRunners[test.Runner.GetBaseRunner().BeaconRoleType] = test.Runner
	v.Network = test.Runner.GetNetwork().(specqbft.Network) // TODO need to align

	var lastErr error
	if !test.DontStartDuty {
		lastErr = v.StartDuty(logger, test.Duty)
	}
	for _, msg := range test.Messages {
		dmsg, err := queue.DecodeSSVMessage(msg)
		if err != nil {
			lastErr = err
			continue
		}
		err = v.ProcessMessage(logger, dmsg)
		if err != nil {
			lastErr = err
		}
	}

	if len(test.ExpectedError) != 0 {
		require.EqualError(t, lastErr, test.ExpectedError, "expected: %v", test.ExpectedError)
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
	require.EqualValues(t, test.PostDutyRunnerStateRoot, hex.EncodeToString(postRoot[:]))
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

	net := v.Network.(specssv.Network)
	broadcastedMsgs := filterPartialSigs(net.(*spectestingutils.TestingNetwork).BroadcastedMsgs)
	require.Len(t, broadcastedMsgs, len(test.OutputMessages))
	index := 0
	for _, msg := range broadcastedMsgs {
		if msg.MsgType != spectypes.SSVPartialSignatureMsgType {
			continue
		}

		msg1 := &spectypes.SignedPartialSignatureMessage{}
		require.NoError(t, msg1.Decode(msg.Data))
		msg2 := test.OutputMessages[index]
		require.Len(t, msg1.Message.Messages, len(msg2.Message.Messages))

		// messages are not guaranteed to be in order so we map them and then test all roots to be equal
		roots := make(map[string]string)
		for i, partialSigMsg2 := range msg2.Message.Messages {
			r2, err := partialSigMsg2.GetRoot()
			require.NoError(t, err)
			if _, found := roots[hex.EncodeToString(r2[:])]; !found {
				roots[hex.EncodeToString(r2[:])] = ""
			} else {
				roots[hex.EncodeToString(r2[:])] = hex.EncodeToString(r2[:])
			}

			partialSigMsg1 := msg1.Message.Messages[i]
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
}

func (test *MsgProcessingSpecTest) overrideStateComparison(t *testing.T) {
	testType := reflect.TypeOf(test).String()
	testType = strings.Replace(testType, "spectest.", "tests.", 1)
	overrideStateComparison(t, test, test.Name, testType)
}

func overrideStateComparison(t *testing.T, test *MsgProcessingSpecTest, name string, testType string) {
	var r runner.Runner
	switch test.Runner.(type) {
	case *runner.AttesterRunner:
		r = &runner.AttesterRunner{}
	case *runner.AggregatorRunner:
		r = &runner.AggregatorRunner{}
	case *runner.ProposerRunner:
		r = &runner.ProposerRunner{}
	case *runner.SyncCommitteeRunner:
		r = &runner.SyncCommitteeRunner{}
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
