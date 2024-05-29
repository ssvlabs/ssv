package spectest

import (
	"context"
	"encoding/hex"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/google/go-cmp/cmp"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	typescomparable "github.com/ssvlabs/ssv-spec/types/testingutils/comparable"
	"github.com/ssvlabs/ssv/integration/qbft/tests"
	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	ssvprotocoltesting "github.com/ssvlabs/ssv/protocol/v2/ssv/testing"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/validator"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
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

func (test *MsgProcessingSpecTest) runPreTesting(ctx context.Context, logger *zap.Logger) (*validator.Validator, *validator.Committee, error) {
	var share *spectypes.Share
	ketSetMap := make(map[phase0.ValidatorIndex]*spectestingutils.TestKeySet)
	if len(test.Runner.GetBaseRunner().Share) == 0 {
		panic("No share in base runner for tests")
	}
	for _, validatorShare := range test.Runner.GetBaseRunner().Share {
		share = validatorShare
		break
	}

	for valIdx, validatorShare := range test.Runner.GetBaseRunner().Share {
		ketSetMap[valIdx] = spectestingutils.KeySetForShare(validatorShare)
	}

	var v *validator.Validator
	var c *validator.Committee
	var lastErr error
	switch test.Runner.(type) {
	case *runner.CommitteeRunner:
		c = baseCommitteeWithRunnerSample(ctx, logger, ketSetMap, test.Runner.(*runner.CommitteeRunner))

		if !test.DontStartDuty {
			lastErr = c.StartDuty(logger, test.Duty.(*spectypes.CommitteeDuty))
		} else {
			c.Runners[test.Duty.DutySlot()] = test.Runner.(*runner.CommitteeRunner)
		}

		for _, msg := range test.Messages {
			dmsg, err := queue.DecodeSignedSSVMessage(msg)
			if err != nil {
				lastErr = err
				continue
			}
			err = c.ProcessMessage(logger, dmsg)
			if err != nil {
				lastErr = err
			}
		}

	default:
		v = ssvprotocoltesting.BaseValidator(logger, spectestingutils.KeySetForShare(share))
		v.DutyRunners[test.Runner.GetBaseRunner().RunnerRoleType] = test.Runner
		v.Network = test.Runner.GetNetwork()

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
	}

	return v, c, lastErr
}

func (test *MsgProcessingSpecTest) RunAsPartOfMultiTest(t *testing.T, logger *zap.Logger) {
	ctx := context.Background()
	v, c, lastErr := test.runPreTesting(ctx, logger)

	if len(test.ExpectedError) != 0 {
		require.EqualError(t, lastErr, test.ExpectedError)
	} else {
		require.NoError(t, lastErr)
	}

	network := &spectestingutils.TestingNetwork{}
	beaconNetwork := tests.NewTestingBeaconNodeWrapped()
	var committee []*spectypes.CommitteeMember
	switch test.Runner.(type) {
	case *runner.CommitteeRunner:
		var runnerInstance *runner.CommitteeRunner
		for _, runner := range c.Runners {
			runnerInstance = runner
			break
		}
		network = runnerInstance.GetNetwork().(*spectestingutils.TestingNetwork)
		beaconNetwork = runnerInstance.GetBeaconNode().(*tests.TestingBeaconNodeWrapped)
		committee = c.Operator.Committee
	default:
		network = v.Network.(*spectestingutils.TestingNetwork)
		committee = v.Operator.Committee
		beaconNetwork = test.Runner.GetBeaconNode()
	}

	// test output message
	spectestingutils.ComparePartialSignatureOutputMessages(t, test.OutputMessages, network.BroadcastedMsgs, committee)

	// test beacon broadcasted msgs
	spectestingutils.CompareBroadcastedBeaconMsgs(t, test.BeaconBroadcastedRoots, beaconNetwork.(*tests.TestingBeaconNodeWrapped).GetBroadcastedRoots())

	// post root
	postRoot, err := test.Runner.GetRoot()
	require.NoError(t, err)

	if test.PostDutyRunnerStateRoot != hex.EncodeToString(postRoot[:]) {
		logger.Error("post runner state not equal", zap.String("state", cmp.Diff(test.Runner, test.PostDutyRunnerState, cmp.Exporter(func(p reflect.Type) bool { return true }))))
	}
}

func (test *MsgProcessingSpecTest) compareBroadcastedBeaconMsgs(t *testing.T) {
	broadcastedRoots := test.Runner.GetBeaconNode().(*tests.TestingBeaconNodeWrapped).GetBroadcastedRoots()
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

var baseCommitteeWithRunnerSample = func(
	ctx context.Context,
	logger *zap.Logger,
	keySetMap map[phase0.ValidatorIndex]*spectestingutils.TestKeySet,
	runnerSample *runner.CommitteeRunner,
) *validator.Committee {

	var keySetSample *spectestingutils.TestKeySet
	for _, ks := range keySetMap {
		keySetSample = ks
		break
	}

	shareMap := make(map[phase0.ValidatorIndex]*spectypes.Share)
	for valIdx, ks := range keySetMap {
		shareMap[valIdx] = spectestingutils.TestingShare(ks, valIdx)
	}

	createRunnerF := func(_ phase0.Slot, shareMap map[phase0.ValidatorIndex]*spectypes.Share) *runner.CommitteeRunner {
		return runner.NewCommitteeRunner(runnerSample.BaseRunner.BeaconNetwork,
			shareMap,
			controller.NewController(
				runnerSample.BaseRunner.QBFTController.Identifier,
				runnerSample.BaseRunner.QBFTController.Share,
				runnerSample.BaseRunner.QBFTController.GetConfig(),
				false,
			),
			runnerSample.GetBeaconNode(),
			runnerSample.GetNetwork(),
			runnerSample.GetSigner(),
			runnerSample.GetOperatorSigner(),
			runnerSample.GetValCheckF(),
		).(*runner.CommitteeRunner)
	}

	return validator.NewCommittee(
		ctx,
		logger,
		runnerSample.GetBaseRunner().BeaconNetwork,
		spectestingutils.TestingOperator(keySetSample),
		spectestingutils.NewTestingVerifier(),
		createRunnerF,
	)
}
